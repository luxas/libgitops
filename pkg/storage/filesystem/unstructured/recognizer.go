package unstructured

import (
	"errors"
	"fmt"
	"io"

	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KubeObjectRecognizer implements ObjectRecognizer.
var _ ObjectRecognizer = KubeObjectRecognizer{}

// KubeObjectRecognizer is a simple implementation of ObjectRecognizer, that
// decodes the given (possibly multi-frame file) into a *metav1.PartialObjectMetadata,
// which allows extracting the ObjectID from any Kube API Machinery-compatible Object.
//
// This operation works even though *metav1.PartialObjectMetadata is not registered
// with the underlying Scheme in any way.
//
// This implementation enforces that .apiVersion, .kind and .metadata.name fields are
// non-empty.
type KubeObjectRecognizer struct {
	// Decoder is a required field in order for RecognizeObjectIDs to function.
	Decoder serializer.Decoder
	// AllowUnrecognized controls whether this implementation allows recognizing
	// GVK combinations not known to the underlying Scheme. Default: false
	AllowUnrecognized bool
	// AllowDuplicates controls whether this implementation allows two exactly similar
	// ObjectIDs in the same file. Default: false
	AllowDuplicates bool
}

func (r KubeObjectRecognizer) RecognizeObjectIDs(_ string, fr serializer.FrameReader) (core.ObjectIDSet, error) {
	if r.Decoder == nil {
		return nil, errors.New("programmer error: KubeObjectRecognizer.Decoder is nil")
	}
	ids := core.NewObjectIDSet()

	for {
		metaObj := &metav1.PartialObjectMetadata{}
		err := r.Decoder.DecodeInto(fr, metaObj)
		if err == io.EOF {
			// If we encountered io.EOF, we know that all is fine and we can exit the for loop and return
			break
		} else if err != nil {
			return nil, err
		}

		// Validate the object info
		gvk := metaObj.GroupVersionKind()
		if gvk.Group == "" && gvk.Version == "" {
			return nil, fmt.Errorf(".apiVersion field must not be empty")
		}
		if gvk.Kind == "" {
			return nil, fmt.Errorf(".kind field must not be empty")
		}
		if metaObj.Name == "" {
			return nil, fmt.Errorf(".metadata.name field must not be empty")
		}
		if !r.AllowUnrecognized && !r.Decoder.SchemeLock().Scheme().Recognizes(gvk) {
			return nil, fmt.Errorf("GroupVersionKind %v not recognized by the scheme", gvk)
		}

		// Create the ObjectID
		id := core.NewObjectID(metaObj.GroupVersionKind(), core.ObjectKeyFromObject(metaObj))
		// Insert it into the set; but error if AllowDuplicates==false and it already existed.
		// Important: As InsertUnique mutates ids, it must be the first if case
		if !ids.InsertUnique(id) && !r.AllowDuplicates {
			return nil, fmt.Errorf("invalid file: two Objects with the same ID: %s", id)
		}
	}

	return ids, nil
}
