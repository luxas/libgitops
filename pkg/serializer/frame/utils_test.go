package frame

import (
	"bytes"
	"context"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
	"github.com/weaveworks/libgitops/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

/*func init() {
	log.SetLogger(zap.New())

	err := tracing.NewBuilder().
		//RegisterStdoutExporter(stdouttrace.WithWriter(io.Discard)).
		RegisterInsecureJaegerExporter("").
		WithLogging(true).
		InstallGlobally()
	if err != nil {
		fmt.Printf("foo")
		os.Exit(1)
	}
}*/

/*TODO
func Test_isStdio(t *testing.T) {
	tmp := t.TempDir()
	f, err := os.Create(filepath.Join(tmp, "foo.txt"))
	require.Nil(t, err)
	defer f.Close()
	tests := []struct {
		name string
		in   interface{}
		want bool
	}{
		{
			name: "os.Stdin",
			in:   os.Stdin,
			want: true,
		},
		{
			name: "os.Stdout",
			in:   os.Stdout,
			want: true,
		},
		{
			name: "os.Stderr",
			in:   os.Stderr,
			want: true,
		},
		{
			name: "*bytes.Buffer",
			in:   bytes.NewBufferString("FooBar"),
		},
		{
			name: "*strings.Reader",
			in:   strings.NewReader("FooBar"),
		},
		{
			name: "*strings.Reader",
			in:   strings.NewReader("FooBar"),
		},
		{
			name: "*os.File",
			in:   f,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStdio(tt.in)
			assert.Equal(t, got, tt.want)
		})
	}
}*/

func someOperation(context.Context) error { return nil }

func doWork(ctx context.Context) error {
	return tracing.FromContextUnnamed(ctx).TraceFunc(ctx, "doWork",
		func(ctx context.Context, span trace.Span) error {
			// ... do some work, get a result ...
			// This will yield a log entry
			result := "wow"
			span.SetAttributes(attribute.String("result", result))

			// .. do more work in someOperation ..
			// if someOperation returns an error, this trace will also have an
			// error registered and logged
			return someOperation(ctx)
		}).Register()
}

func doWork2(ctx context.Context) error {
	log := logr.FromContextOrDiscard(ctx)
	log.WithName("doWork")

	// ... do some work, get a result ...
	result := "wow"
	log.Info("got a result", "result", result)

	// .. do more work in someOperation ..
	err := someOperation(ctx)
	if err != nil {
		log.Error(err, "got error from someOperation")
		return err
	}
	return nil
}

func TestFromConstructors(t *testing.T) {
	yamlPath := filepath.Join(t.TempDir(), "foo.yaml")
	str := "foo: bar\n"
	byteContent := []byte(str)
	err := ioutil.WriteFile(yamlPath, byteContent, 0644)
	require.Nil(t, err)

	ctx := tracing.BackgroundTracingContext()
	// FromFile -- found
	got, err := NewYAMLReader(content.FromFile(yamlPath)).ReadFrame(ctx)
	assert.Nil(t, err)
	assert.Equal(t, str, string(got))
	// FromFile -- already closed
	f := content.FromFile(yamlPath)
	f.RawCloser().Close() // deliberately close the file before giving it to the reader
	got, err = NewYAMLReader(f).ReadFrame(ctx)
	assert.ErrorIs(t, err, fs.ErrClosed)
	assert.Empty(t, got)
	// FromFile -- not found
	got, err = NewYAMLReader(content.FromFile(filepath.Join(t.TempDir(), "notexist.yaml"))).ReadFrame(ctx)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.Empty(t, got)
	// FromBytes
	got, err = NewYAMLReader(content.FromBytes(byteContent)).ReadFrame(ctx)
	assert.Nil(t, err)
	assert.Equal(t, byteContent, got)
	// FromString
	got, err = NewYAMLReader(content.FromString(str)).ReadFrame(ctx)
	assert.Nil(t, err)
	assert.Equal(t, str, string(got))
	assert.Nil(t, tracing.ForceFlushGlobal(context.Background(), 0))
	t.Error("foo")
}

func TestToIoWriteCloser(t *testing.T) {
	var buf bytes.Buffer
	closeRec := &recordingCloser{}
	cw := content.NewWriter(content.IoWriteCloser{Writer: &buf, Closer: closeRec})
	w := NewYAMLWriter(cw, &ReaderWriterOptions{MaxFrameSize: testYAMLlen})
	ctx := tracing.BackgroundTracingContext()
	iow := ToIoWriteCloser(ctx, w)

	byteContent := []byte(testYAML)
	n, err := iow.Write(byteContent)
	assert.Len(t, byteContent, n)
	assert.Nil(t, err)

	// Check closing forwarding
	assert.Nil(t, iow.Close())
	assert.Equal(t, 1, closeRec.count)

	// Try writing again
	overflowContent := []byte(testYAML + testYAML)
	n, err = iow.Write(overflowContent)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, content.ErrFrameSizeOverflow)
	// Assume the writer has been closed only once
	assert.Equal(t, 1, closeRec.count)
	assert.Equal(t, buf.String(), yamlSep+string(byteContent))

	assert.Nil(t, tracing.ForceFlushGlobal(context.Background(), 0))
	t.Error("foo")
}

func TestReadFrameList(t *testing.T) {
	r := NewYAMLReader(content.FromString(messyYAML))
	ctx := context.Background()
	// Happy case
	fr, err := ReadFrameList(ctx, r)
	assert.Equal(t, FrameList{[]byte(testYAML), []byte(testYAML)}, fr)
	assert.Nil(t, err)

	// Non-happy case
	r = NewJSONReader(content.FromString(messyJSON), &ReaderWriterOptions{MaxFrameSize: testJSONlen - 1})
	fr, err = ReadFrameList(ctx, r)
	assert.Len(t, fr, 0)
	assert.ErrorIs(t, err, content.ErrFrameSizeOverflow)
}

func TestWriteFrameList(t *testing.T) {
	var buf bytes.Buffer
	// TODO: Automatically get the name of the writer passed in, to avoid having to name
	// everything. i.e. content.NewWriterName(string, io.Writer)
	cw := content.NewWriter(&buf)
	w := NewYAMLWriter(cw)
	ctx := context.Background()
	// Happy case
	err := WriteFrameList(ctx, w, FrameList{[]byte(testYAML), []byte(testYAML)})
	assert.Equal(t, buf.String(), yamlSep+testYAML+yamlSep+testYAML)
	assert.Nil(t, err)

	// Non-happy case
	buf.Reset()
	w = NewJSONWriter(cw, &ReaderWriterOptions{MaxFrameSize: testJSONlen})
	err = WriteFrameList(ctx, w, FrameList{[]byte(testJSON), []byte(testJSON2)})
	assert.Equal(t, buf.String(), testJSON)
	assert.ErrorIs(t, err, content.ErrFrameSizeOverflow)
}
