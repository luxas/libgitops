package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/weaveworks/libgitops/pkg/filter"
	"github.com/weaveworks/libgitops/pkg/serializer"
	"github.com/weaveworks/libgitops/pkg/storage/backend"
	"github.com/weaveworks/libgitops/pkg/storage/core"
	patchutil "github.com/weaveworks/libgitops/pkg/util/patch"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	utilerrs "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO: Pass an ObjectID that contains all PartialObjectMetadata info for "downstream" consumers
// that can make use of it by "casting up".

var (
	// ErrUnsupportedPatchType is returned when an unsupported patch type is used
	ErrUnsupportedPatchType = errors.New("unsupported patch type")
)

type Reader interface {
	client.Reader
	BackendReader() backend.Reader
}

type Writer interface {
	client.Writer
	BackendWriter() backend.Writer
}

type StatusClient interface {
	client.StatusClient
	BackendStatusWriter() backend.StatusWriter
}

// Client is an interface for persisting and retrieving API objects to/from a backend
// One Client instance handles all different Kinds of Objects
type Client interface {
	Reader
	Writer
	// TODO: StatusClient
	//client.Client
}

// NewGeneric constructs a new Generic client
// TODO: Construct the default patcher from the given scheme, make patcher an opt instead
func NewGeneric(backend backend.Backend, patcher serializer.Patcher) (*Generic, error) {
	if backend == nil {
		return nil, fmt.Errorf("backend is mandatory")
	}
	return &Generic{backend, patcher}, nil
}

// Generic implements the Client interface
type Generic struct {
	backend backend.Backend
	patcher serializer.Patcher
}

var _ Client = &Generic{}

func (c *Generic) Backend() backend.Backend      { return c.backend }
func (c *Generic) BackendReader() backend.Reader { return c.backend }
func (c *Generic) BackendWriter() backend.Writer { return c.backend }

// Get returns a new Object for the resource at the specified kind/uid path, based on the file content.
// In order to only extract the metadata of this object, pass in a *metav1.PartialObjectMetadata
func (c *Generic) Get(ctx context.Context, key core.ObjectKey, obj core.Object) error {
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	return c.backend.Get(ctx, obj)
}

// List lists Objects for the specific kind. Optionally, filters can be applied (see the filter package
// for more information, e.g. filter.NameFilter{} and filter.UIDFilter{})
// You can also pass in an *unstructured.UnstructuredList to get an unknown type's data or
// *metav1.PartialObjectMetadataList to just get the metadata of all objects of the specified gvk.
// If you do specify either an *unstructured.UnstructuredList or *metav1.PartialObjectMetadataList,
// you need to populate TypeMeta with the GVK you want back.
// TODO: Check if this works with metav1.List{}
// TODO: Create constructors for the different kinds of lists?
func (c *Generic) List(ctx context.Context, list core.ObjectList, opts ...client.ListOption) error {
	// This call will verify that list actually is a List type.
	gvk, err := serializer.GVKForList(list, c.Backend().Scheme())
	if err != nil {
		return err
	}
	// This applies both upstream and custom options
	listOpts := (&ListOptions{}).ApplyOptions(opts)

	// Get namespacing info
	gk := gvk.GroupKind()
	namespaced, err := c.Backend().Storage().Namespacer().IsNamespaced(gk)
	if err != nil {
		return err
	}

	// By default, only search the given namespace. It is fully valid for this to be an
	// empty string: it is the only
	namespaces := sets.NewString(listOpts.Namespace)
	// However, if the GroupKind is namespaced, and the given "filter namespace" in list
	// options is empty, it means that one should list all namespaces
	if namespaced && listOpts.Namespace == "" {
		namespaces, err = c.Backend().ListNamespaces(ctx, gk)
		if err != nil {
			return err
		}
	} else if !namespaced && listOpts.Namespace != "" {
		return errors.New("invalid namespace option: cannot filter namespace for root-spaced object")
	}

	allIDs := []core.UnversionedObjectID{}
	for ns := range namespaces {
		ids, err := c.Backend().ListObjectIDs(ctx, gk, ns)
		if err != nil {
			return err
		}
		allIDs = append(allIDs, ids...)
	}

	// TODO: Is this a good default? Need to balance mem usage and speed. This is prob. too much
	ch := make(chan core.Object, len(allIDs))

	objs := make([]kruntime.Object, 0, len(allIDs))
	go func() {
		for o := range ch {
			objs = append(objs, o)
		}
	}()

	createFunc := createObject(gvk, c.Backend().Scheme())
	if serializer.IsPartialObjectList(list) {
		createFunc = createPartialObject(gvk)
	} else if serializer.IsUnstructuredList(list) {
		createFunc = createUnstructuredObject(gvk)
	}
	// Start one goroutine per ID, and get back an aggregate error
	err = c.processKeys(ctx, allIDs, &listOpts.FilterOptions, createFunc, ch)
	// Always unconditionally stop the channel after this, we know there won't
	// be any more writes to it. This will terminate the for-range loop above.
	close(ch)
	if err != nil {
		return err
	}

	// Populate the List's Items field with the objects returned
	return meta.SetList(list, objs)
}

func (c *Generic) Create(ctx context.Context, obj core.Object, _ ...client.CreateOption) error {
	return c.backend.Create(ctx, obj)
}

func (c *Generic) Update(ctx context.Context, obj core.Object, _ ...client.UpdateOption) error {
	return c.backend.Update(ctx, obj)
}

// Patch performs a strategic merge patch on the object with the given UID, using the byte-encoded patch given
func (c *Generic) Patch(ctx context.Context, obj core.Object, patch core.Patch, _ ...client.PatchOption) error {
	// Fail-fast: We must never save metadata-only structs
	if serializer.IsPartialObject(obj) {
		return backend.ErrCannotSaveMetadata
	}

	// Acquire the patch data from the "desired state" object given now, i.e. in MergeFrom{}
	// TODO: Shall we require GVK to be present here using a meta interpreter?
	patchJSON, err := patch.Data(obj)
	if err != nil {
		return err
	}

	// Load the current latest state into obj temporarily, before patching it
	// This also validates the GVK, name and namespace.
	if err := c.backend.Get(ctx, obj); err != nil {
		return err
	}

	// Get the right BytePatcher for this patch type
	// TODO: Make this return an error
	bytePatcher := patchutil.BytePatcherForType(patch.Type())
	if bytePatcher == nil {
		return fmt.Errorf("patch type not supported: %s", patch.Type())
	}

	// Apply the patch into the object using the given byte patcher
	if unstruct, ok := obj.(kruntime.Unstructured); ok {
		// TODO: Provide an option for the schema
		err = c.patcher.ApplyOnUnstructured(bytePatcher, patchJSON, unstruct, nil)
	} else {
		err = c.patcher.ApplyOnStruct(bytePatcher, patchJSON, obj)
	}
	if err != nil {
		return err
	}

	// Perform an update internally, similar to what .Update would yield
	// TODO: Maybe write to the Storage conditionally? using DryRun all
	return c.Update(ctx, obj)
}

// Delete removes an Object from the backend
// PartialObjectMetadata should work here.
func (c *Generic) Delete(ctx context.Context, obj core.Object, _ ...client.DeleteOption) error {
	return c.backend.Delete(ctx, obj)
}

// DeleteAllOf deletes all matched resources by first doing a List() operation on the given GVK of
// obj (obj is not used for anything else) and the given filters in opts. Only the Partial Meta
func (c *Generic) DeleteAllOf(ctx context.Context, obj core.Object, opts ...client.DeleteAllOfOption) error {
	// This applies both upstream and custom options, and propagates the options correctly to both
	// List() and Delete()
	customDeleteAllOpts := (&DeleteAllOfOptions{}).ApplyOptions(opts)

	// Get the GVK of the object
	gvk, err := serializer.GVKForObject(c.Backend().Scheme(), obj)
	if err != nil {
		return err
	}

	// List all matched objects for the given ListOptions, and GVK.
	// UnstructuredList is used here so that we can use filters that operate on fields
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)
	if err := c.List(ctx, list, customDeleteAllOpts); err != nil {
		return err
	}

	// Loop through all of the matched items, and Delete them one-by-one
	for i := range list.Items {
		if err := c.Delete(ctx, &list.Items[i], customDeleteAllOpts); err != nil {
			return err
		}
	}
	return nil
}

// Scheme returns the scheme this client is using.
func (c *Generic) Scheme() *kruntime.Scheme {
	return c.backend.Scheme()
}

// RESTMapper returns the rest this client is using. For now, this returns nil, so don't use.
func (c *Generic) RESTMapper() meta.RESTMapper {
	return nil
}

type newObjectFunc func() (core.Object, error)

func createObject(gvk core.GroupVersionKind, scheme *kruntime.Scheme) newObjectFunc {
	return func() (core.Object, error) {
		return NewObjectForGVK(gvk, scheme)
	}
}

func createPartialObject(gvk core.GroupVersionKind) newObjectFunc {
	return func() (core.Object, error) {
		obj := &metav1.PartialObjectMetadata{}
		obj.SetGroupVersionKind(gvk)
		return obj, nil
	}
}

func createUnstructuredObject(gvk core.GroupVersionKind) newObjectFunc {
	return func() (core.Object, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		return obj, nil
	}
}

func (c *Generic) processKeys(ctx context.Context, ids []core.UnversionedObjectID, filterOpts *filter.FilterOptions, fn newObjectFunc, output chan core.Object) error {
	goroutines := []func() error{}
	for _, id := range ids {
		goroutines = append(goroutines, c.processKey(ctx, id, filterOpts, fn, output))
	}
	return utilerrs.AggregateGoroutines(goroutines...)
}

func (c *Generic) processKey(ctx context.Context, id core.UnversionedObjectID, filterOpts *filter.FilterOptions, fn newObjectFunc, output chan core.Object) func() error {
	return func() error {
		// Create a new object, and decode into it using Get
		obj, err := fn()
		if err != nil {
			return err
		}

		if err := c.Get(ctx, id.ObjectKey(), obj); err != nil {
			return err
		}

		// Match the object against the filters
		matched, err := filterOpts.Match(obj)
		if err != nil {
			return err
		}
		if matched {
			output <- obj
		}

		return nil
	}
}