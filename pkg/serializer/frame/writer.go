package frame

import (
	"context"
	"sync"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
	"github.com/weaveworks/libgitops/pkg/tracing"
	"go.opentelemetry.io/otel/trace"
)

func newHighlevelWriter(w Writer, opts *WriterOptions) Writer {
	return &highlevelWriter{
		writer:   w,
		writerMu: &sync.Mutex{},
		opts:     opts,
	}
}

type highlevelWriter struct {
	writer   Writer
	writerMu *sync.Mutex
	opts     *WriterOptions
	// frameCount counts the amount of successful frames written
	frameCount int64
}

func (w *highlevelWriter) WriteFrame(ctx context.Context, frame []byte) error {
	w.writerMu.Lock()
	defer w.writerMu.Unlock()

	return tracing.FromContext(ctx, w).TraceFunc(ctx, "WriteFrame", func(ctx context.Context, span trace.Span) error {
		// Refuse to write too large frames
		if int64(len(frame)) > w.opts.MaxFrameSize {
			return content.MakeFrameSizeOverflowError(w.opts.MaxFrameSize)
		}
		// Refuse to write more than the maximum amount of frames
		if w.frameCount >= w.opts.MaxFrames {
			return MakeFrameCountOverflowError(w.opts.MaxFrames)
		}

		// Sanitize the frame
		// TODO: Maybe create a composite writer that actually reads the given frame first, to
		// fully sanitize/validate it, and first then write the frames out using the writer?
		frame, err := w.opts.Sanitizer.Sanitize(w.FramingType(), frame)
		if err != nil {
			return err
		}

		// Register the amount of (sanitized) bytes and call the underlying Writer
		span.SetAttributes(content.SpanAttrReadContent(frame)...)

		// Catch empty frames
		if len(frame) == 0 {
			return nil
		}

		err = w.writer.WriteFrame(ctx, frame)

		// Increase the frame counter, if the write was successful
		if err == nil {
			w.frameCount += 1
		}
		return err
	}).Register()
}

func (w *highlevelWriter) FramingType() FramingType { return w.writer.FramingType() }
func (w *highlevelWriter) Close(ctx context.Context) error {
	return closeWithTrace(ctx, w.writer, w).Register()
}
