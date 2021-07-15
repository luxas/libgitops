package frame

import (
	"context"

	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
)

// DefaultFactory is the default variant of Factory capable
// of creating YAML- and JSON-compatible Readers and Writers.
//
// If ReaderWriterOptions.MaxFrames == 1, any FramingType can
// be supplied, not just YAML or JSON. ReadFrame will then read
// and return all data in the underlying io.Reader. WriteFrame
// will write the given frame to the underlying io.Writer as-is.
type DefaultFactory struct{}

func (f DefaultFactory) NewReader(framingType FramingType, r content.Reader, opts ...ReaderOption) Reader {
	// Build the options from the defaults
	o := defaultReaderOpts().ApplyOptions(opts)
	// Wrap r in a io.NopCloser if it isn't closable. Mark os.Std{in,out,err} as not closable.
	//rc, hasCloser := toReadCloser(r)
	// Wrap the low-level Reader from lowlevelFromReadCloser in a composite highlevelReader applying common policy
	return newHighlevelReader(f.lowlevelFromReadCloser(framingType, r, o), o)
}

func (DefaultFactory) lowlevelFromReadCloser(framingType FramingType, r content.Reader, o *ReaderOptions) Reader {
	switch framingType {
	case FramingTypeYAML:
		return newYAMLReader(r, o)
	case FramingTypeJSON:
		return newJSONReader(r, o)
	/*case FramingTypeSingle:
	// Unconditionally set MaxFrames to 1
	o.MaxFrames = 1
	return newSingleReader(framingType, r, o)*/
	default:
		if o.MaxFrames == 1 {
			return newSingleReader(framingType, r, o)
		}
		return newErrReader(framingType, MakeUnsupportedFramingTypeError(framingType))
	}
}

// defaultReaderFactory is the variable used in public methods.
var defaultReaderFactory ReaderFactory = DefaultFactory{}

// NewReader returns a Reader for the given FramingType and underlying io.Read(Clos)er.
//
// This is a shorthand for DefaultFactory{}.NewReader(framingType, r, opts...)
func NewReader(framingType FramingType, r content.Reader, opts ...ReaderOption) Reader {
	return defaultReaderFactory.NewReader(framingType, r, opts...)
}

// NewYAMLReader returns a Reader that supports both YAML and JSON. Frames are separated by "---\n"
//
// This is a shorthand for NewReader(FramingTypeYAML, rc, opts...)
func NewYAMLReader(r content.Reader, opts ...ReaderOption) Reader {
	return NewReader(FramingTypeYAML, r, opts...)
}

// NewJSONReader returns a Reader that supports both JSON. Objects are read from the stream one-by-one,
// each object making up its own frame.
//
// This is a shorthand for NewReader(FramingTypeJSON, rc, opts...)
func NewJSONReader(r content.Reader, opts ...ReaderOption) Reader {
	return NewReader(FramingTypeJSON, r, opts...)
}

func newErrReader(framingType FramingType, err error) Reader {
	return &errReader{framingType.ToFramingTyped(), &nopCloser{}, err}
}

// errReader always returns an error
type errReader struct {
	FramingTyped
	Closer
	err error
}

func (r *errReader) ReadFrame(context.Context) ([]byte, error) { return nil, r.err }

//func (r *errReader) ContentMetadata() content.Metadata         { return content.NewMetadata() }

// nopCloser returns nil when Close(ctx) is called
type nopCloser struct{}

func (*nopCloser) Close(context.Context) error { return nil }
