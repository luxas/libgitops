package frame

import (
	"io"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

// Documentation below attached to NewWriter.
func (f DefaultFactory) NewWriter(framingType FramingType, w content.Writer, opts ...WriterOption) Writer {
	// Build the concrete options struct from defaults and modifiers
	o := defaultWriterOpts().ApplyOptions(opts)
	// Wrap the writer in a layer that provides tracing and mutex capabilities
	return newHighlevelWriter(f.newFromContentWriter(framingType, w, o), o)
}

func (DefaultFactory) newFromContentWriter(framingType FramingType, w content.Writer, o *WriterOptions) Writer {
	switch framingType {
	case FramingTypeYAML:
		return newDelegatingWriter(framingType, w.Wrap(func(underlying io.WriteCloser) io.Writer {
			return json.YAMLFramer.NewFrameWriter(underlying)
		}), o)
	case FramingTypeJSON:
		return newDelegatingWriter(framingType, w.Wrap(func(underlying io.WriteCloser) io.Writer {
			return json.Framer.NewFrameWriter(underlying)
		}), o)
	/*case FramingTypeSingle:
	// Unconditionally set MaxFrames to 1
	o.MaxFrames = 1
	return newSingleWriter(framingType, wc, o)*/
	default:
		if o.MaxFrames == 1 {
			return newSingleWriter(framingType, w, o)
		}
		return newErrWriter(framingType, MakeUnsupportedFramingTypeError(framingType))
	}
}

// defaultWriterFactory is the variable used in public methods.
var defaultWriterFactory WriterFactory = DefaultFactory{}

// NewWriter returns a new Writer for the given Writer and FramingType.
// The returned Writer is thread-safe.
func NewWriter(framingType FramingType, w content.Writer, opts ...WriterOption) Writer {
	return defaultWriterFactory.NewWriter(framingType, w, opts...)
}

// NewYAMLWriter returns a Writer that writes YAML frames separated by "---\n"
//
// This call is the same as NewWriter(FramingTypeYAML, w, opts...)
func NewYAMLWriter(w content.Writer, opts ...WriterOption) Writer {
	return NewWriter(FramingTypeYAML, w, opts...)
}

// NewJSONWriter returns a Writer that writes JSON frames without separation
// (i.e. "{ ... }{ ... }{ ... }" on the wire)
//
// This call is the same as NewWriter(FramingTypeYAML, w)
func NewJSONWriter(w content.Writer, opts ...WriterOption) Writer {
	return NewWriter(FramingTypeJSON, w, opts...)
}

type nopWriteCloser struct{ io.Writer }

func (wc *nopWriteCloser) Close() error { return nil }
