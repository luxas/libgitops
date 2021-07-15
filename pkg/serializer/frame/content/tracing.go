package content

import "go.opentelemetry.io/otel/attribute"

const (
	SpanAttributeKeyByteLength      = "byteLength"
	SpanAttributeKeyByteContent     = "byteContent"
	SpanAttributeKeyContentMetadata = "contentMetadata"
)

func SpanAttrReadContent(b []byte) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(SpanAttributeKeyByteContent, string(b)),
		attribute.Int64(SpanAttributeKeyByteLength, int64(len(b))),
	}
}

func SpanAttrContentMetadata(m Metadata) attribute.KeyValue {
	return attribute.Any(SpanAttributeKeyContentMetadata, m)
}
