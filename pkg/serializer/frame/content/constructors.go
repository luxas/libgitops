package content

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing/iotest"
)

// newErrReader makes a Reader implementation that only returns the given error on Read()
func newErrReader(err error, opts ...MIMEHeaderOption) Reader {
	return NewReader(iotest.ErrReader(err), opts...)
}

const (
	stdinPath  = "/dev/stdin"
	stdoutPath = "/dev/stdout"
	stderrPath = "/dev/stderr"
)

// FromFile returns an io.ReadCloser from the given file, or an io.ReadCloser which returns
// the given file open error when read.
func FromFile(filePath string, opts ...MIMEHeaderOption) Reader {
	// Support stdin
	if filePath == "-" || filePath == stdinPath {
		// Mark the source as /dev/stdin
		opts = append(opts, SetMIMEHeaderOption{XContentLocationKey, stdinPath})
		// TODO: Maybe have a way to override the TracerName through Metadata?
		return NewReader(os.Stdin, opts...)
	}

	// Make sure the path is absolute
	filePath, err := filepath.Abs(filePath)
	if err != nil {
		return newErrReader(err, opts...)
	}
	// Report the file path in the X-Content-Location header
	opts = append(opts, SetMIMEHeaderOption{XContentLocationKey, filePath})

	// Open the file
	f, err := os.Open(filePath)
	if err != nil {
		return newErrReader(err, opts...)
	}
	fi, err := f.Stat()
	if err != nil {
		return newErrReader(err, opts...)
	}

	// Register the Content-Length header
	opts = append(opts, setContentLength(fi.Size()))

	return NewReader(f, opts...)
}

// FromBytes returns an io.Reader from the given byte content.
func FromBytes(content []byte, opts ...MIMEHeaderOption) Reader {
	// Register the Content-Length
	opts = append(opts, setContentLength(int64(len(content))))
	// Read from a *bytes.Reader
	return NewReader(bytes.NewReader(content), opts...)
}

// FromString returns an io.Reader from the given string content.
func FromString(content string, opts ...MIMEHeaderOption) Reader {
	// Register the Content-Length
	opts = append(opts, setContentLength(int64(len(content))))
	// Read from a *strings.Reader
	return NewReader(strings.NewReader(content), opts...)
}

/*func FromHTTPResponse(resp *http.Response, opts ...MIMEHeaderOption) Reader {
	resp.Location()
}*/

func setContentLength(len int64) MIMEHeaderOption {
	return SetMIMEHeaderOption{ContentLengthKey, strconv.FormatInt(len, 10)}
}

/*func ToFile(filePath string) io.WriteCloser {
	// Shorthands for pipe IO
	if filePath == "-" || filePath == stdoutPath {
		return os.Stdout
	}
	if filePath == stderrPath {
		return os.Stderr
	}

	// Make sure all directories are created
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return errIoWriteCloser(err)
	}

	// Create or truncate the file
	f, err := os.Create(filePath)
	if err != nil {
		return errIoWriteCloser(err)
	}
	return f
}*/
