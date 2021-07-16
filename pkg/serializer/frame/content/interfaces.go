package content

import (
	"context"
	"io"
)

// underlying is the underlying stream of the Reader.
// If the returned io.Reader does not implement io.Closer,
// the underlying.Close() method will be re-used.
type WrapReaderFunc func(underlying io.ReadCloser) io.Reader

type WrapWriterFunc func(underlying io.WriteCloser) io.Writer

type WrapReaderToSegmentFunc func(underlying io.ReadCloser) RawSegmentReader

// Reader is a tracing-capable and metadata-bound io.Reader and io.Closer
// wrapper. It is NOT thread-safe by default. It supports introspection
// of composite ReadClosers. The TracerProvider from the given context
// is used.
//
// The Reader reads the current span from the given context, and uses that
// span's TracerProvider to create a Tracer and then also a new Span for
// the current operation.
type Reader interface {
	// These call the underlying Set/ClearContext functions before/after
	// reads and closes, and then uses the underlying io.ReadCloser.
	// If the underlying Reader doesn't support closing, the returned
	// Close method will only log a "CloseNoop" trace and exit with err == nil.
	WithContext(ctx context.Context) io.ReadCloser

	// This reader supports registering metadata about the content it
	// is reading.
	MetadataBound

	// Wrap returns a new Reader with io.ReadCloser B that reads from
	// the current Reader's underlying io.ReadCloser A. If the returned
	// B is an io.ReadCloser or this Reader's HasCloser() is true,
	// HasCloser() of the returned Reader will be true, otherwise false.
	Wrap(fn WrapReaderFunc) Reader
	WrapSegment(fn WrapReaderToSegmentFunc) SegmentReader
}

type readerInternal interface {
	Reader
	RawReader() io.Reader
	RawCloser() io.Closer
}

type RawSegmentReader interface {
	Read() ([]byte, error)
}

type ClosableRawSegmentReader interface {
	RawSegmentReader
	io.Closer
}

type SegmentReader interface {
	WithContext(ctx context.Context) ClosableRawSegmentReader

	MetadataBound
}

// In the future, one can implement a WrapSegment function that is of
// the following form:
// WrapSegment(name string, fn WrapSegmentFunc) SegmentReader
// where WrapSegmentFunc is func(underlying ClosableRawSegmentReader) RawSegmentReader
// This allows chaining simple composite SegmentReaders

type segmentReaderInternal interface {
	SegmentReader
	RawSegmentReader() RawSegmentReader
	RawCloser() io.Closer
}

type Writer interface {
	WithContext(ctx context.Context) io.WriteCloser

	// This writer supports registering metadata about the content it
	// is writing and the destination it is writing to.
	MetadataBound

	Wrap(fn WrapWriterFunc) Writer
}

type writerInternal interface {
	Writer
	RawWriter() io.Writer
	RawCloser() io.Closer
}