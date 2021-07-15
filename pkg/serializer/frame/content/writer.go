package content

import (
	"context"
	"fmt"
	"io"

	"github.com/weaveworks/libgitops/pkg/tracing"
	"go.opentelemetry.io/otel/trace"
)

type IoWriteCloser struct {
	io.Writer
	io.Closer
}

func (wc IoWriteCloser) TracerName() string {
	return fmt.Sprintf("content.IoWriteCloser{%T, %T}", wc.Writer, wc.Closer)
}

var _ tracing.TracerNamed = IoReadCloser{}

func NewWriter(w io.Writer, opts ...MIMEHeaderOption) Writer {
	// If it already is a Writer, just return it
	ww, ok := w.(Writer)
	if ok {
		return ww
	}

	// Use the closer if available
	c, _ := w.(io.Closer)
	// Never close stdio
	if isStdio(w) {
		c = nil
	}
	m := &metadataBound{NewMetadata(opts...)}

	return &writer{
		MetadataBound: m,
		write: &writeContextLockImpl{
			w:          w,
			metaGetter: m,
			// underlyingLock is nil
		},
		close: &closeContextLockImpl{
			c:          c,
			metaGetter: m,
			// underlyingLock is nil
		},
	}
}

type writer struct {
	MetadataBound
	write *writeContextLockImpl
	close *closeContextLockImpl
}

func (w *writer) WithContext(ctx context.Context) io.WriteCloser {
	return IoWriteCloser{&writerWithContext{w.write, ctx}, &closerWithContext{w.close, ctx}}
}
func (w *writer) RawWriter() io.Writer { return w.write.w }
func (w *writer) RawCloser() io.Closer { return w.close.c }

func (w *writer) Wrap(wrapFn WrapWriterFunc) Writer {
	newWriter := wrapFn(IoWriteCloser{w.write, w.close})
	if newWriter == nil {
		panic("newWriter must not be nil")
	}
	// If an io.Closer is not returned, close this
	// Reader's stream instead. Importantly enough,
	// a trace will be registered for both this
	// Reader, and the returned one.
	newCloser, ok := newWriter.(io.Closer)
	if !ok {
		newCloser = w.close
	}

	mb := NewMetadataBound(w.ContentMetadata().Clone())

	return &writer{
		MetadataBound: mb,
		write: &writeContextLockImpl{
			w:              newWriter,
			metaGetter:     mb,
			underlyingLock: w.write,
		},
		close: &closeContextLockImpl{
			c:              newCloser,
			metaGetter:     mb,
			underlyingLock: w.close,
		},
	}
}

type writerWithContext struct {
	write *writeContextLockImpl
	ctx   context.Context
}

func (w *writerWithContext) Write(p []byte) (n int, err error) {
	w.write.setContext(w.ctx)
	n, err = w.write.Write(p)
	w.write.clearContext()
	return
}

type writeContextLockImpl struct {
	contextLockImpl
	w              io.Writer
	metaGetter     MetadataBound
	underlyingLock contextLock
}

func (r *writeContextLockImpl) Write(p []byte) (n int, err error) {
	ft := tracing.FuncTracerFromContext(r.ctx, r.w)
	err = ft.TraceFunc(r.ctx, "Write", func(ctx context.Context, span trace.Span) error {
		var tmperr error
		if r.underlyingLock != nil {
			r.underlyingLock.setContext(ctx)
		}
		n, tmperr = r.w.Write(p)
		if r.underlyingLock != nil {
			r.underlyingLock.clearContext()
		}
		// Register metadata in the span
		// TODO: Maybe register len(p) too?
		span.SetAttributes(SpanAttrReadContent(p[:n])...)
		return tmperr
	}, trace.WithAttributes(SpanAttrContentMetadata(r.metaGetter.ContentMetadata()))).Register()
	return
}
