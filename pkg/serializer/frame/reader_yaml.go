package frame

import (
	"bufio"
	"context"
	"io"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// newYAMLReader creates a "low-level" YAML Reader from the given io.ReadCloser.
func newYAMLReader(r content.Reader, opts *ReaderOptions) Reader {
	// Default maxFrameSize, to be sure
	// TODO: Do we need this?
	maxFrameSize := content.OrDefaultMaxFrameSize(opts.MaxFrameSize)

	// Limit the amount of bytes that can be read from the underlying io.ReadCloser.
	// Allow reading 4 bytes more than the maximum frame size, because the "---\n"
	// also counts for the IoLimitedReader.
	// TODO: Fix the upstream YAMLReader to not return "---\n" in the Read call if that's
	// in the beginning of the stream.
	r, resetCounter := content.WrapLimited(r, maxFrameSize+4)

	// Wrap the underlying, limited reader in a *yaml.YAMLReader, with a mandatory *bufio.Reader
	// in between due to how the YAMLReader works.
	cr := r.WrapSegment(func(underlying io.ReadCloser) content.RawSegmentReader {
		// One good thing to note about the behavior of the *bufio.Reader is that although
		// the IoLimitedReader returns a FrameOverflowErr when it overflows, the buffer
		// _does not_ directly return the error upstream, but will try reading from the
		// IoLimitedReader once again due to the following statement in *bufio.Reader.ReadLine:
		// "ReadLine either returns a non-nil line or it returns an error, never both."
		// See k8s.io/apimachinery/pkg/util/yaml.LineReader.Read for more context.
		return yaml.NewYAMLReader(bufio.NewReader(underlying))
	})

	return &yamlReader{
		cr:           cr,
		maxFrameSize: maxFrameSize,
		resetCounter: resetCounter,
	}
}

// yamlReader is capable of returning individual YAML documents from the underlying io.ReadCloser.
// The returned YAML documents are sanitized such that they are non-empty, doesn't contain any
// leading or trailing "---" strings or whitespace (including newlines). There is always a trailing
// newline, however. The returned frame byte count <= opts.MaxFrameSize.
//
// Note: This Reader is a so-called "low-level" one. It doesn't do tracing, mutex locking, or
// proper closing logic. It must be wrapped by a composite, high-level Reader like highlevelReader.
type yamlReader struct {
	cr           content.SegmentReader
	maxFrameSize int64
	resetCounter func()
}

func (r *yamlReader) ReadFrame(ctx context.Context) ([]byte, error) {
	// Read one YAML document from the underlying YAMLReader. The YAMLReader reads the file line-by-line
	// using a *bufio.Reader. The *bufio.Reader reads in turn from an IoLimitedReader, which limits the
	// amount of bytes that can be read to avoid an endless data attack (which the YAMLReader doesn't
	// protect against). If the frame is too large, errors.Is(err, ErrFrameSizeOverflow) == true. Once
	// ErrFrameSizeOverflow has been returned once, it'll be returned for all consecutive calls (by design),
	// because the byte counter is never reset.
	frame, err := r.cr.WithContext(ctx).Read()
	if err != nil {
		return nil, err
	}

	// Reset now that we know a good frame has been read (err == nil)
	r.resetCounter()

	// Enforce this "final" frame size <= maxFrameSize, as the limit on the IoLimitedReader was a bit less
	// restrictive (also allowed reading the YAML document separator).
	if int64(len(frame)) > r.maxFrameSize {
		return nil, content.MakeFrameSizeOverflowError(r.maxFrameSize)
	}

	return frame, nil
}

func (r *yamlReader) FramingType() FramingType        { return FramingTypeYAML }
func (r *yamlReader) Close(ctx context.Context) error { return r.cr.WithContext(ctx).Close() }
