package frame

import (
	"context"
	"io"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
)

func newSingleReader(framingType FramingType, r content.Reader, o *ReaderOptions) Reader {
	// Make sure not more than this set of bytes can be read
	r, _ = content.WrapLimited(r, o.MaxFrameSize)
	return &singleReader{
		FramingTyped: framingType.ToFramingTyped(),
		r:            r,
	}
}

func newSingleWriter(framingType FramingType, w content.Writer, _ *WriterOptions) Writer {
	return &singleWriter{
		FramingTyped: framingType.ToFramingTyped(),
		w:            w,
	}
}

// singleReader implements reading a single frame (up to a certain limit) from an io.ReadCloser.
// It MUST be wrapped in a higher-level composite Reader like the highlevelReader to satisfy the
// Reader interface correctly.
type singleReader struct {
	FramingTyped
	r           content.Reader
	hasBeenRead bool
}

// Read the whole frame from the underlying io.Reader, up to a given limit
func (r *singleReader) ReadFrame(ctx context.Context) ([]byte, error) {
	// Only allow reading once
	if !r.hasBeenRead {
		// Read the whole frame from the underlying io.Reader, up to a given amount
		frame, err := io.ReadAll(r.r.WithContext(ctx))
		// Mark we have read the frame
		r.hasBeenRead = true
		return frame, err
	}
	return nil, io.EOF
}

func (r *singleReader) Close(ctx context.Context) error { return r.r.WithContext(ctx).Close() }

// singleWriter implements writing a single frame to an io.WriteCloser.
// It MUST be wrapped in a higher-level composite Reader like the highlevelWriter to satisfy the
// Writer interface correctly.
type singleWriter struct {
	FramingTyped
	w              content.Writer
	hasBeenWritten bool
}

func (w *singleWriter) WriteFrame(ctx context.Context, frame []byte) error {
	// Only allow writing once
	if !w.hasBeenWritten {
		// The first time, write the whole frame to the underlying writer
		n, err := w.w.WithContext(ctx).Write(frame)
		// Mark we have written the frame
		w.hasBeenWritten = true
		// Guard against short frames
		return catchShortWrite(n, err, frame)
	}
	// This really should never happen, because the higher-level Writer should ensure
	// that the frame is not ever written downstream if the maximum frame count is
	// exceeded, which it always is here as MaxFrames == 1 and w.hasBeenWritten == true.
	// In any case, for consistency, return io.ErrClosedPipe.
	return io.ErrClosedPipe
}

func (w *singleWriter) Close(ctx context.Context) error { return w.w.WithContext(ctx).Close() }
