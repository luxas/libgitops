package content

import (
	"encoding/json"
	"net/textproto"
	"net/url"
	"strconv"
)

type metadataBound struct{ m Metadata }

func (b *metadataBound) ContentMetadata() Metadata { return b.m }

func NewMetadataBound(m Metadata) MetadataBound { return &metadataBound{m} }

const (
	XContentLocationKey = "X-Content-Location"
	XFramingTypeKey     = "X-Framing-Type"

	ContentLengthKey = "Content-Length"
	ContentTypeKey   = "Content-Type"
)

type MIMEHeaderOption interface {
	ApplyToMIMEHeader(target MIMEHeader)
}

var _ MIMEHeaderOption = SetMIMEHeaderOption{}

type SetMIMEHeaderOption struct{ Key, Value string }

func (o SetMIMEHeaderOption) ApplyToMIMEHeader(target MIMEHeader) {
	target.Set(o.Key, o.Value)
}

func NewMetadata(opts ...MIMEHeaderOption) Metadata {
	return NewMetadataForHeader(textproto.MIMEHeader{}, opts...)
}

func NewMetadataForHeader(header textproto.MIMEHeader, opts ...MIMEHeaderOption) Metadata {
	return contentMetadata{MIMEHeader: header}.ApplyOptions(opts...)
}

type contentMetadata struct {
	textproto.MIMEHeader
}

// Express the string-string map interface of the net/textproto.MIMEHeader map
type MIMEHeader interface {
	Add(key, value string)
	Set(key, value string)
	Get(key string) string
	Values(key string) []string
	Del(key string)
}

// Metadata is the interface that's common to contentMetadataOptions and a wrapper
// around a HTTP request.
type Metadata interface {
	MIMEHeader

	// ApplyOptions applies the given Options to itself and returns itself.
	ApplyOptions(opts ...MIMEHeaderOption) Metadata
	// ContentLength retrieves the standard "Content-Length" header
	ContentLength() (int64, bool)
	// ContentType retrieves the standard "Content-Type" header
	ContentType() (string, bool)
	// ContentLocation retrieves the custom "X-Content-Location" header
	ContentLocation() (*url.URL, bool)
	// FramingType retrieves the custom "X-Framing-Type" header
	FramingType() (string, bool)

	// Clone makes a deep copy of the Metadata
	Clone() Metadata
}

var _ Metadata = contentMetadata{}

var _ json.Marshaler = contentMetadata{}

func (m contentMetadata) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.MIMEHeader)
}

func (m contentMetadata) ApplyOptions(opts ...MIMEHeaderOption) Metadata {
	for _, opt := range opts {
		opt.ApplyToMIMEHeader(m)
	}
	return m
}

func (m contentMetadata) GetString(key string) (string, bool) {
	if len(m.MIMEHeader.Values(key)) == 0 {
		return "", false
	}
	return m.MIMEHeader.Get(key), true
}

func (m contentMetadata) GetInt64(key string) (int64, bool) {
	i, err := strconv.ParseInt(m.MIMEHeader.Get(key), 10, 64)
	if err != nil {
		return 0, false
	}
	return i, true
}

func (m contentMetadata) GetURL(key string) (*url.URL, bool) {
	str, ok := m.GetString(key)
	if !ok {
		return nil, false
	}
	u, err := url.Parse(str)
	if err != nil {
		return nil, false
	}
	return u, true
}

func (m contentMetadata) ContentLength() (int64, bool) {
	return m.GetInt64(ContentLengthKey)
}

func (m contentMetadata) ContentType() (string, bool) {
	return m.GetString(ContentTypeKey)
}

func (m contentMetadata) FramingType() (string, bool) {
	return m.GetString(XFramingTypeKey)
}

func (m contentMetadata) ContentLocation() (*url.URL, bool) {
	return m.GetURL(XContentLocationKey)
}

func (m contentMetadata) Clone() Metadata {
	m2 := make(textproto.MIMEHeader, len(m.MIMEHeader))
	for k, v := range m.MIMEHeader {
		m2[k] = v
	}
	return contentMetadata{m2}
}

type MetadataBound interface {
	// ContentMetadata
	ContentMetadata() Metadata
}

/*
	Content-Encoding
	Content-Length
	Content-Type
	Last-Modified
	ETag
*/
