package serializer

import (
	"context"
	"errors"

	"github.com/weaveworks/libgitops/pkg/serializer/frame"
	"github.com/weaveworks/libgitops/pkg/serializer/frame/content"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type ContentType string

// TODO: Generate with go-enum
type PreserveComments string

const (
	PreserveCommentsNever      PreserveComments = "Never"
	PreserveCommentsBestEffort PreserveComments = "BestEffort"
	PreserveCommentsAlways     PreserveComments = "Always"
)

type DecodeOption interface {
	ApplyToDecode(*DecodeOptions)
}

type DecodeOptions struct {
	// Mappers can e.g. default or convert objects on their way to the user
	Mappers          []DecodeResultMapper
	Strict           *bool
	PreserveComments *PreserveComments
}

type EncodeOptions struct {
	IndentSpaces uint8
}

type Object interface {
	runtime.Object
	metav1.Object
}

type ObjectList interface {
	runtime.Object
	metav1.ListInterface
}

type Decoder interface {
	Decode(ctx context.Context, fr frame.Reader, opts ...DecodeOption) DecodeResults
}

type DecodeResults chan DecodeResult

type DecodeResultType int

const (
	DecodeResultObject DecodeResultType = iota
	DecodeResultObjectList
	DecodeResultUnknown
	DecodeResultError
)

type DecodeResult interface {
	Type() DecodeResultType
	Get() interface{}
}

// Sample mappers are DefaultingMapper, ConvertToHubMapper and ListFlattenerMapper
type DecodeResultMapper interface {
	Map(DecodeResults) DecodeResults
}

type IntoDecoder interface {
	DecodeInto(fr frame.Reader) IntoDecodeBuilder

	Converter() Converter
	Decoder() Decoder
}

type Converter interface {
	// in and out must be non-nil.
	// This must be an object, not a list
	Convert(ctx context.Context, in, out Object) error
}

type ObjectDecoder interface {
	Decode(ctx context.Context, content []byte, opts ...DecodeOption) (runtime.Object, error)
	SupportedContentTypes() []ContentType // sets.String
}

type ContentTyper interface {
	ResolveContentType(metadata content.Metadata, content []byte) (ContentType, error)
}

func foo() {

}

var (
	ErrAmbiguousMatches = errors.New("ambiguous match, deserialized object matches multiple into's")
	ErrNotEnoughIntos   = errors.New("not all decoded objects matched the given into's")
)

type IntoDecodeBuilder interface {
	// Object can be a pointer to a typed struct, *metav1.PartialObjectMetadata,
	// or *unstructured.Unstructured. In case it obj is a *metav1.PartialObjectMetadata
	// or *unstructured.Unstructured, TypeMeta MUST be populated manually before calling
	// this method. If both name and namespace is empty, matching will be done only by
	// GroupVersionKind, but if at least one of them is set, matching will happen only
	// if both of the fields equal.
	Object(obj Object) IntoDecodeBuilder
	// ObjectList can be a pointer to list with typed structs as items,
	// *metav1.PartialObjectMetadataList, or *unstructured.UnstructuredList.
	// In case it list is a *metav1.PartialObjectMetadataList or *unstructured.UnstructuredList,
	// TypeMeta MUST be populated manually before calling this method.
	// If list is a v1.List
	ObjectList(list ObjectList) IntoDecodeBuilder
	Unknown(unk *[]runtime.Unknown) IntoDecodeBuilder

	Do(ctx context.Context) error
}

type ListClassifier interface {
	IsList(obj interface{}) (list, ok bool)
}

var _ ObjectList = &metav1.List{}
var r = &runtime.RawExtension{}
