package core

import "k8s.io/apimachinery/pkg/runtime/schema"

// NewUnversionedObjectID creates a new UnversionedObjectID from the given GroupKind and ObjectKey.
func NewUnversionedObjectID(gk GroupKind, key ObjectKey) UnversionedObjectID {
	return unversionedObjectID{gk, key}
}

type unversionedObjectID struct {
	gk  GroupKind
	key ObjectKey
}

func (o unversionedObjectID) GroupKind() GroupKind                { return o.gk }
func (o unversionedObjectID) ObjectKey() ObjectKey                { return o.key }
func (o unversionedObjectID) WithVersion(version string) ObjectID { return objectID{o, version} }

// NewObjectID creates a new ObjectID from the given GroupVersionKind and ObjectKey.
func NewObjectID(gvk GroupVersionKind, key ObjectKey) ObjectID {
	return objectID{unversionedObjectID{gvk.GroupKind(), key}, gvk.Version}
}

type objectID struct {
	unversionedObjectID
	version string
}

func (o objectID) GroupVersionKind() schema.GroupVersionKind { return o.gk.WithVersion(o.version) }