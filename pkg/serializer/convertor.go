package serializer

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	webhookconversion "sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
)

// TODO: Unit-test the converter implementation directly, not just indirectly by usage in the decode mechanism

var (
	errOutMustBeHub       = errors.New("if in object is Convertible, out must be Hub")
	errInMustBeHub        = errors.New("if out object is Convertible, in must be Hub")
	errMustNotHaveTwoHubs = errors.New("in and out must not both be Hubs")
	errObjMustNotBeBoth   = errors.New("given object must not implement both the Convertible and Hub interfaces")
)

func newConverter(scheme *runtime.Scheme) *converter {
	return &converter{
		scheme:    scheme,
		convertor: newObjectConvertor(scheme, true),
	}
}

// converter implements the Converter interface
type converter struct {
	scheme    *runtime.Scheme
	convertor *objectConvertor
}

// Convert converts in directly into out. out should be an empty object of the destination type.
// Both objects must be of the same kind and either have autogenerated conversions registered, or
// be controller-runtime CRD-style implementers of the sigs.k8s.io/controller-runtime/pkg/conversion.Hub
// and Convertible interfaces. In the case of CRD Convertibles and Hubs, there must be one Convertible and
// one Hub given in the in and out arguments. No defaulting is performed.
func (c *converter) Convert(in, out runtime.Object) error {
	return c.convertor.Convert(in, out, nil)
}

// ConvertIntoNew creates a new object for the specified groupversionkind, uses Convert(in, out)
// under the hood, and returns the new object. No defaulting is performed.
// TODO: If needed, this function could only accept a GroupVersion, not GroupVersionKind
func (c *converter) ConvertIntoNew(in runtime.Object, gvk schema.GroupVersionKind) (runtime.Object, error) {
	// Create a new object of the given gvk
	obj, err := c.scheme.New(gvk)
	if err != nil {
		return nil, err
	}

	// Convert into that object
	if err := c.Convert(in, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// ConvertToHub converts the given in object to either the internal version (for API machinery "classic")
// or the sigs.k8s.io/controller-runtime/pkg/conversion.Hub for the given conversion.Convertible object in
// the "in" argument. No defaulting is performed.
func (c *converter) ConvertToHub(in runtime.Object) (runtime.Object, error) {
	return c.convertor.ConvertToVersion(in, nil)
}

func newObjectConvertor(scheme *runtime.Scheme, doConversion bool) *objectConvertor {
	return &objectConvertor{scheme, doConversion}
}

// objectConvertor implements runtime.ObjectConvertor. See k8s.io/apimachinery/pkg/runtime/serializer/versioning.go for
// how this objectConvertor is used (e.g. in codec.Decode())
type objectConvertor struct {
	scheme       *runtime.Scheme
	doConversion bool
}

// Convert attempts to convert one object into another, or returns an error. This
// method does not mutate the in object, but the in and out object might share data structures,
// i.e. the out object cannot be mutated without mutating the in object as well.
// The context argument will be passed to all nested conversions.
// This function might return errors of type *CRDConversionError.
func (c *objectConvertor) Convert(in, out, context interface{}) error {
	// This function is called at DecodeInto-time, and should convert the decoded object into
	// the into object.

	// TODO: Unit test that typed errors are returned properly

	// If "in" is a controller-runtime CRD convertible, check if "out" is a Hub, and convert. Otherwise
	// return an error
	inConvertible, inOk := in.(conversion.Convertible)
	if inOk {
		// Require out to be a Hub, and convert
		outHub, outOk := out.(conversion.Hub)
		if !outOk {
			return NewCRDConversionError(nil, CRDConversionErrorCauseInvalidArgs, errOutMustBeHub)
		}
		return c.convertIntoHub(inConvertible, outHub)
	}

	// If "out" is a controller-runtime CRD convertible, check if "in" is a Hub, and convert. Otherwise
	// return an error
	outConvertible, outOk := out.(conversion.Convertible)
	if outOk {
		// Require out to be a Hub, and convert
		inHub, inOk := in.(conversion.Hub)
		if !inOk {
			return NewCRDConversionError(nil, CRDConversionErrorCauseInvalidArgs, errInMustBeHub)
		}
		return c.convertFromHub(inHub, outConvertible)
	}

	// Catch the edge case if someone passes two Hubs into both in and out
	// TODO: This should have an unit test
	_, inIsHub := in.(conversion.Hub)
	_, outIsHub := out.(conversion.Hub)
	if inIsHub && outIsHub {
		return NewCRDConversionError(nil, CRDConversionErrorCauseInvalidArgs, errMustNotHaveTwoHubs)
	}

	// Do normal conversion
	return c.scheme.Convert(in, out, context)
}

func (c *objectConvertor) convertIntoHub(in conversion.Convertible, out conversion.Hub) error {
	// Make sure the in object is convertible into a Hub
	inGVK, err := validateConvertible(in, c.scheme)
	if err != nil {
		return err
	}

	// TODO: Unit test that typed errors are returned properly

	// Convert the hub into the convertible
	if err := in.ConvertTo(out); err != nil {
		return NewCRDConversionError(&inGVK, CRDConversionErrorCauseConvertTo, err)
	}

	// Populate the Hub's TypeMeta
	return populateGVK(out, c.scheme)
}

func (c *objectConvertor) convertFromHub(in conversion.Hub, out conversion.Convertible) error {
	// TODO: Unit-test this function
	// Make sure the out object is convertible into a Hub
	outGVK, err := validateConvertible(out, c.scheme)
	if err != nil {
		return err
	}

	// Convert the hub into the convertible
	if err := out.ConvertFrom(in); err != nil {
		return NewCRDConversionError(&outGVK, CRDConversionErrorCauseConvertFrom, err)
	}

	// Populate the Convertible's TypeMeta
	return populateGVK(out, c.scheme)
}

// ConvertToVersion takes the provided object and converts it the provided version. This
// method does not mutate the in object, but the in and out object might share data structures,
// i.e. the out object cannot be mutated without mutating the in object as well.
// This method is similar to Convert() but handles specific details of choosing the correct
// output version.
// This function might return errors of type *CRDConversionError.
func (c *objectConvertor) ConvertToVersion(in runtime.Object, groupVersioner runtime.GroupVersioner) (runtime.Object, error) {
	// This function is called at Decode(All)-time. If we requested a conversion to internal, just proceed
	// as before, using the scheme's ConvertToVersion function. But if we don't want to convert the newly-decoded
	// external object, we can just do nothing and the object will stay unconverted.
	// doConversion is always true in the Encode codepath.
	if !c.doConversion {
		// DeepCopy the object to make sure that although in would be somehow modified, it doesn't affect out
		return in.DeepCopyObject(), nil
	}

	// At this point we know we are either in the ConvertToHub Decode(All) codepath, or Encode
	// Check whether "in" is a CRD-type object
	convertible, isConvertible := in.(conversion.Convertible)
	_, isHub := in.(conversion.Hub)

	// Return quickly if neither of the objects are CRD-types, using the "classic" API machinery
	if !isHub && !isConvertible {
		// Convert normally using the specified groupversion
		return c.scheme.ConvertToVersion(in, groupVersioner)

	} else if isHub && isConvertible { // Validate that the object isn't crazy and implements both interfaces
		return nil, NewCRDConversionError(nil, CRDConversionErrorCauseInvalidArgs, errObjMustNotBeBoth)
	}

	// We now know that either isHub or isConvertible is true, but not both
	// If we are in the Decode codepath, the groupVersioner will be internal
	// We'll need to take special care to convert the object into a Hub
	if groupVersioner == runtime.InternalGroupVersioner {
		// As a "ConvertToHub" was asked for, and the in object already is a Hub, just return a deepcopy
		if isHub {
			return in.DeepCopyObject(), nil
		}

		// Otherwise, convert it to a Hub
		return c.convertToHub(convertible)
	}

	// At this point we are in the encode codepath. The groupversioner given specifies what
	// groupVersion to convert into

	// Get what group version was asked for
	gv, ok := groupVersioner.(schema.GroupVersion)
	if !ok {
		return nil, fmt.Errorf("couldn't get groupversion from groupversioner: %v", groupVersioner)
	}

	// Get the groupversionkind for the in object
	inGVK, err := gvkForObject(c.scheme, in)
	if err != nil {
		return nil, fmt.Errorf("couldn't get GVK for hub: %w", err)
	}
	// Assume the in and out (Hub and Convertible) kinds match (in encoded form)
	outGVK := gv.WithKind(inGVK.Kind)

	// Create the out object
	out, err := c.scheme.New(outGVK)
	if err != nil {
		return nil, fmt.Errorf("can't create new obj of gvk %s: %w", outGVK, err)
	}

	// Run the generic convert in-into-out function, which will properly handle this CRD case
	if err := c.Convert(in, out, nil); err != nil {
		return nil, err
	}

	return out, nil
}

func (c *objectConvertor) convertToHub(in conversion.Convertible) (runtime.Object, error) {
	// Make sure the object is convertible into a Hub
	currentGVK, err := validateConvertible(in, c.scheme)
	if err != nil {
		return nil, err
	}

	// Find the Hub type for the given current gvk
	hub, targetGVK, err := findHubType(currentGVK, c.scheme)
	if err != nil {
		return nil, NewCRDConversionError(&targetGVK, CRDConversionErrorCauseSchemeSetup, err)
	}

	// Convert from the in object to the hub and return it
	if err := in.ConvertTo(hub); err != nil {
		return nil, NewCRDConversionError(&targetGVK, CRDConversionErrorCauseConvertTo, err)
	}
	// Populate the new gvk information on the newly-converted Hub
	hub.GetObjectKind().SetGroupVersionKind(targetGVK)

	return hub, nil
}

func (c *objectConvertor) ConvertFieldLabel(gvk schema.GroupVersionKind, label, value string) (string, string, error) {
	// just forward this call, not applicable to this implementation
	return c.scheme.ConvertFieldLabel(gvk, label, value)
}

// validateConvertible verifies that the in object is actually a properly configured Convertible (with a
// conversion path to Hub), and returns the type's gvk if there are no errors. A *CRDConversionError might
// be returned from this function
func validateConvertible(in conversion.Convertible, scheme *runtime.Scheme) (schema.GroupVersionKind, error) {
	// Fetch the current in object's GVK
	gvk, err := gvkForObject(scheme, in)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}

	// If the version should be converted, construct a new version of the object to convert into,
	// convert and finally add to the list
	ok, err := webhookconversion.IsConvertible(scheme, in)
	if err != nil {
		return gvk, NewCRDConversionError(&gvk, CRDConversionErrorCauseSchemeSetup, err)
	}
	if !ok {
		return gvk, NewCRDConversionError(&gvk, CRDConversionErrorCauseSchemeSetup, nil)
	}
	return gvk, nil
}

// findHubType looks in the scheme's all known types for a matching Hub type for the given current gvk
func findHubType(currentGVK schema.GroupVersionKind, scheme *runtime.Scheme) (conversion.Hub, schema.GroupVersionKind, error) {
	// Loop through all the groupversions for the kind to find the one with the Hub
	for gvk := range scheme.AllKnownTypes() {
		// Skip any non-similar groupkinds
		if gvk.GroupKind().String() != currentGVK.GroupKind().String() {
			continue
		}
		// Skip the same version that the convertible has
		if gvk.Version == currentGVK.Version {
			continue
		}

		// Create an object for the certain gvk
		obj, err := scheme.New(gvk)
		if err != nil {
			continue
		}

		// Try to cast it to a Hub, and save it if we need
		hub, ok := obj.(conversion.Hub)
		if !ok {
			continue
		}
		return hub, gvk, nil
	}
	return nil, schema.GroupVersionKind{}, fmt.Errorf("no matching Hub target type for convertible of gvk %s", currentGVK)
}

// populateGVK finds the gvk of the objects and populates TypeMeta with that information
func populateGVK(obj runtime.Object, scheme *runtime.Scheme) error {
	// Fetch the current in object's GVK
	gvk, err := gvkForObject(scheme, obj)
	if err != nil {
		return err
	}
	// Populate the new gvk information on the newly-converted Hub
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	return nil
}
