package frame

import (
	"context"
	"io"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
)

func newDelegatingWriter(framingType FramingType, w content.Writer, opts *WriterOptions) Writer {
	return &delegatingWriter{
		FramingTyped: framingType.ToFramingTyped(),
		w:            w,
		opts:         opts,
	}
}

// delegatingWriter is an implementation of the Writer interface
type delegatingWriter struct {
	FramingTyped
	w    content.Writer
	opts *WriterOptions
}

func (w *delegatingWriter) WriteFrame(ctx context.Context, frame []byte) error {
	// Write the frame to the underlying writer
	n, err := w.w.WithContext(ctx).Write(frame)
	// Guard against short writes
	return catchShortWrite(n, err, frame)
}

func (w *delegatingWriter) Close(ctx context.Context) error { return w.w.WithContext(ctx).Close() }

func newErrWriter(framingType FramingType, err error) Writer {
	return &errWriter{framingType.ToFramingTyped(), &nopCloser{}, err}
}

type errWriter struct {
	FramingTyped
	Closer
	err error
}

func (w *errWriter) WriteFrame(context.Context, []byte) error { return w.err }

func catchShortWrite(n int, err error, frame []byte) error {
	if n < len(frame) && err == nil {
		err = io.ErrShortWrite
	}
	return err
}
