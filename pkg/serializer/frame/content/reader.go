package content

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/weaveworks/libgitops/pkg/tracing"
	"go.opentelemetry.io/otel/trace"
)

type contextLock interface {
	setContext(ctx context.Context)
	clearContext()
}

type contextLockImpl struct {
	ctx context.Context
}

func (l *contextLockImpl) setContext(ctx context.Context) { l.ctx = ctx }
func (l *contextLockImpl) clearContext()                  { l.ctx = nil }

type readContextLockImpl struct {
	contextLockImpl
	r              io.Reader
	metaGetter     MetadataBound
	underlyingLock contextLock
}

func (r *readContextLockImpl) Read(p []byte) (n int, err error) {
	ft := tracing.FuncTracerFromContext(r.ctx, r.r)
	err = ft.TraceFunc(r.ctx, "Read", func(ctx context.Context, span trace.Span) error {
		var tmperr error
		if r.underlyingLock != nil {
			r.underlyingLock.setContext(ctx)
		}
		n, tmperr = r.r.Read(p)
		if r.underlyingLock != nil {
			r.underlyingLock.clearContext()
		}
		// Register metadata in the span
		span.SetAttributes(SpanAttrReadContent(p[:n])...)
		return tmperr
	}, trace.WithAttributes(SpanAttrContentMetadata(r.metaGetter.ContentMetadata()))).RegisterCustom(SpanRegisterReadError)
	return
}

type closeContextLockImpl struct {
	contextLockImpl
	c              io.Closer
	metaGetter     MetadataBound
	underlyingLock contextLock
}

func (c *closeContextLockImpl) Close() error {
	spanName := "Close"
	if c.c == nil {
		spanName = "CloseNoop"
	}

	ft := tracing.FuncTracerFromContext(c.ctx, c.c)
	return ft.TraceFunc(c.ctx, spanName, func(ctx context.Context, _ trace.Span) error {
		// Don't close if c.c is nil
		if c.c == nil {
			return nil
		}

		if c.underlyingLock != nil {
			c.underlyingLock.setContext(ctx)
		}
		// Close the underlying resource
		err := c.c.Close()
		if c.underlyingLock != nil {
			c.underlyingLock.clearContext()
		}
		return err
	}, trace.WithAttributes(SpanAttrContentMetadata(c.metaGetter.ContentMetadata()))).Register()
}

type reader struct {
	MetadataBound
	read  *readContextLockImpl
	close *closeContextLockImpl
}

type readerWithContext struct {
	read *readContextLockImpl
	ctx  context.Context
}

func (r *readerWithContext) Read(p []byte) (n int, err error) {
	r.read.setContext(r.ctx)
	n, err = r.read.Read(p)
	r.read.clearContext()
	return
}

type closerWithContext struct {
	close *closeContextLockImpl
	ctx   context.Context
}

func (r *closerWithContext) Close() error {
	r.close.setContext(r.ctx)
	err := r.close.Close()
	r.close.clearContext()
	return err
}

func (r *reader) WithContext(ctx context.Context) io.ReadCloser {
	return IoReadCloser{&readerWithContext{r.read, ctx}, &closerWithContext{r.close, ctx}}
}
func (r *reader) RawReader() io.Reader { return r.read.r }
func (r *reader) RawCloser() io.Closer { return r.close.c }

// Maybe allow adding extra attributes at the end?
func (r *reader) Wrap(wrapFn WrapReaderFunc) Reader {
	newReader := wrapFn(IoReadCloser{r.read, r.close})
	if newReader == nil {
		panic("newReader must not be nil")
	}
	// If an io.Closer is not returned, close this
	// Reader's stream instead. Importantly enough,
	// a trace will be registered for both this
	// Reader, and the returned one.
	newCloser, ok := newReader.(io.Closer)
	if !ok {
		newCloser = r.close
	}

	mb := NewMetadataBound(r.ContentMetadata().Clone())

	return &reader{
		MetadataBound: mb,
		read: &readContextLockImpl{
			r:              newReader,
			metaGetter:     mb,
			underlyingLock: r.read,
		},
		close: &closeContextLockImpl{
			c:              newCloser,
			metaGetter:     mb,
			underlyingLock: r.close,
		},
	}
}

func (r *reader) WrapSegment(wrapFn WrapReaderToSegmentFunc) SegmentReader {
	newSegmentReader := wrapFn(IoReadCloser{r.read, r.close})
	if newSegmentReader == nil {
		panic("newSegmentReader must not be nil")
	}

	// If an io.Closer is not returned, close this
	// Reader's stream instead. Importantly enough,
	// a trace will be registered for both this
	// Reader, and the returned one.
	newCloser, ok := newSegmentReader.(io.Closer)
	if !ok {
		newCloser = r.close
	}

	mb := NewMetadataBound(r.ContentMetadata().Clone())

	return &segmentReader{
		MetadataBound: mb,
		read: &readSegmentContextLockImpl{
			r:              newSegmentReader,
			metaGetter:     mb,
			underlyingLock: r.read,
		},
		close: &closeContextLockImpl{
			c:              newCloser,
			metaGetter:     mb,
			underlyingLock: r.close,
		},
	}
}

func NewReader(r io.Reader, opts ...MIMEHeaderOption) Reader {
	// If it already is a Reader, just return it
	rr, ok := r.(Reader)
	if ok {
		return rr
	}

	// Use the closer if available
	c, _ := r.(io.Closer)
	// Never close stdio
	if isStdio(r) {
		c = nil
	}
	m := &metadataBound{NewMetadata(opts...)}

	return &reader{
		MetadataBound: m,
		read: &readContextLockImpl{
			r:          r,
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

func isStdio(s interface{}) bool {
	f, ok := s.(*os.File)
	if !ok {
		return false
	}
	return int(f.Fd()) < 3
}

// SpanRegisterReadError registers io.EOF as an "event", and other errors as "unknown errors" in the trace
func SpanRegisterReadError(span trace.Span, err error) error {
	// Register the error with the span. EOF is expected at some point,
	// hence, register that as an event instead of an error
	if errors.Is(err, io.EOF) {
		span.AddEvent("EOF")
	} else if err != nil {
		span.RecordError(err)
	}
	return err
}

type IoReadCloser struct {
	io.Reader
	io.Closer
}

func (rc IoReadCloser) TracerName() string {
	return fmt.Sprintf("content.IoReadCloser{%T, %T}", rc.Reader, rc.Closer)
}

var _ tracing.TracerNamed = IoReadCloser{}
