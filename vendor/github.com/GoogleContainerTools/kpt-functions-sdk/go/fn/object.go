// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fn

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn/internal"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// KubeObject presents a k8s object.
type KubeObject struct {
	SubObject
}

// ParseKubeObject parses input byte slice to a KubeObject.
func ParseKubeObject(in []byte) (*KubeObject, error) {
	doc, err := internal.ParseDoc(in)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input bytes: %w", err)
	}
	objects, err := doc.Elements()
	if err != nil {
		return nil, fmt.Errorf("failed to extract objects: %w", err)
	}
	if len(objects) != 1 {
		return nil, fmt.Errorf("expected exactly one object, got %d", len(objects))
	}
	rlMap := objects[0]
	return asKubeObject(rlMap), nil
}

// GetOrDie gets the value for a nested field located by fields. A pointer must
// be passed in, and the value will be stored in ptr. If the field doesn't
// exist, the ptr will be set to nil. It will panic if it encounters any error.
func (o *SubObject) GetOrDie(ptr interface{}, fields ...string) {
	_, err := o.Get(ptr, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// NestedBool returns the bool value, if the field exist and a potential error.
func (o *SubObject) NestedBool(fields ...string) (bool, bool, error) {
	var val bool
	found, err := o.Get(&val, fields...)
	return val, found, err
}

// NestedBoolOrDie returns the bool value at `fields`. An empty string will be
// returned if the field is not found. It will panic if encountering any errors.
func (o *SubObject) NestedBoolOrDie(fields ...string) bool {
	val, _, err := o.NestedBool(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedString returns the string value, if the field exist and a potential error.
func (o *SubObject) NestedString(fields ...string) (string, bool, error) {
	var val string
	found, err := o.Get(&val, fields...)
	return val, found, err
}

// NestedStringOrDie returns the string value at fields. An empty string will be
// returned if the field is not found. It will panic if encountering any errors.
func (o *SubObject) NestedStringOrDie(fields ...string) string {
	val, _, err := o.NestedString(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedFloat64 returns the float64 value, if the field exist and a potential error.
func (o *SubObject) NestedFloat64(fields ...string) (float64, bool, error) {
	var val float64
	found, err := o.Get(&val, fields...)
	return val, found, err
}

// NestedFloat64OrDie returns the string value at fields. 0 will be
// returned if the field is not found. It will panic if encountering any errors.
func (o *SubObject) NestedFloat64OrDie(fields ...string) float64 {
	val, _, err := o.NestedFloat64(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedInt64 returns the int64 value, if the field exist and a potential error.
func (o *SubObject) NestedInt64(fields ...string) (int64, bool, error) {
	var val int64
	found, err := o.Get(&val, fields...)
	return val, found, err
}

// NestedInt64OrDie returns the string value at fields. An empty string will be
// returned if the field is not found. It will panic if encountering any errors.
func (o *SubObject) NestedInt64OrDie(fields ...string) int64 {
	val, _, err := o.NestedInt64(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedSlice accepts a slice of `fields` which represents the path to the slice component and
// return a slice of SubObjects as the first return value; whether the component exists or
// not as the second return value, and errors as the third return value.
func (o *SubObject) NestedSlice(fields ...string) (SliceSubObjects, bool, error) {
	var mapVariant *internal.MapVariant
	if len(fields) > 1 {
		m, found, err := o.obj.GetNestedMap(fields[:len(fields)-1]...)
		if err != nil || !found {
			return nil, found, err
		}
		mapVariant = m
	} else {
		mapVariant = o.obj
	}
	sliceVal, found, err := mapVariant.GetNestedSlice(fields[len(fields)-1])
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	if !found {
		return nil, found, nil
	}
	objects, err := sliceVal.Elements()
	if err != nil {
		return nil, found, err
	}
	var val []*SubObject
	for _, obj := range objects {
		val = append(val, &SubObject{obj: obj})
	}
	return val, true, nil
}

// NestedSliceOrDie accepts a slice of `fields` which represents the path to the slice component and
// return a slice of SubObjects.
// - It returns nil if the fields does not exist.
// - It panics with errSubObjectFields error if the field is not a slice type.
func (o *SubObject) NestedSliceOrDie(fields ...string) SliceSubObjects {
	val, _, err := o.NestedSlice(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedMap returns a map[string]string value of a nested field, false if not found and an error if not a map[string]string type.
func (o *SubObject) NestedStringMap(fields ...string) (map[string]string, bool, error) {
	var variant map[string]string
	found, err := o.Get(&variant, fields...)
	if err != nil || !found {
		return nil, found, err
	}
	return variant, found, err
}

// NestedStringMapOrDie returns a map[string]string value of a nested field, if the fields does not exist, it returns
// empty map[string]string. It will panic if the fields are not map[string]string type.
func (o *SubObject) NestedStringMapOrDie(fields ...string) map[string]string {
	val, _, err := o.NestedStringMap(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// NestedMap returns a map[string]string value of a nested field, false if not found and an error if not a map[string]string type.
func (o *SubObject) NestedStringSlice(fields ...string) ([]string, bool, error) {
	var variant []string
	found, err := o.Get(&variant, fields...)
	if err != nil || !found {
		return nil, found, err
	}
	return variant, found, err
}

// NestedMapOrDie returns a map[string]string value of a nested field.
// It will panic if the fields are not map[string]string type.
func (o *SubObject) NestedStringSliceOrDie(fields ...string) []string {
	val, _, err := o.NestedStringSlice(fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
	return val
}

// RemoveNestedFieldOrDie removes the field located by fields if found. It will panic if it
// encounters any error.
func (o *SubObject) RemoveNestedFieldOrDie(fields ...string) {
	if _, err := o.RemoveNestedField(fields...); err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// RemoveNestedField removes the field located by fields if found. It returns if the field
// is found and a potential error.
func (o *SubObject) RemoveNestedField(fields ...string) (bool, error) {
	found, err := func() (bool, error) {
		if o == nil {
			return false, fmt.Errorf("the object doesn't exist")
		}
		return o.obj.RemoveNestedField(fields...)
	}()
	if err != nil {
		return found, fmt.Errorf("unable to remove fields %v with error: %w", fields, err)
	}
	return found, nil
}

// Get gets the value for a nested field located by fields. A pointer must be
// passed in, and the value will be stored in ptr. ptr can be a concrete type
// (e.g. string, []corev1.Container, []string, corev1.Pod, map[string]string) or
// a yaml.RNode. yaml.RNode should be used if you are dealing with comments that
// is more than what LineComment, HeadComment, SetLineComment and
// SetHeadComment can handle. It returns if the field is found and a
// potential error.
func (o *SubObject) Get(ptr interface{}, fields ...string) (bool, error) {
	found, err := func() (bool, error) {
		if o == nil {
			return false, fmt.Errorf("the object doesn't exist")
		}
		if ptr == nil || reflect.ValueOf(ptr).Kind() != reflect.Ptr {
			return false, fmt.Errorf("ptr must be a pointer to an object")
		}

		if rn, ok := ptr.(*yaml.RNode); ok {
			val, found, err := o.obj.GetNestedValue(fields...)
			if err != nil || !found {
				return found, err
			}
			rn.SetYNode(val.Node())
			return found, err
		}

		switch k := reflect.TypeOf(ptr).Elem().Kind(); k {
		case reflect.Struct, reflect.Map:
			m, found, err := o.obj.GetNestedMap(fields...)
			if err != nil || !found {
				return found, err
			}
			err = m.Node().Decode(ptr)
			return found, err
		case reflect.Slice:
			s, found, err := o.obj.GetNestedSlice(fields...)
			if err != nil || !found {
				return found, err
			}
			err = s.Node().Decode(ptr)
			return found, err
		case reflect.String:
			s, found, err := o.obj.GetNestedString(fields...)
			if err != nil || !found {
				return found, err
			}
			*(ptr.(*string)) = s
			return found, nil
		case reflect.Int, reflect.Int64:
			i, found, err := o.obj.GetNestedInt(fields...)
			if err != nil || !found {
				return found, err
			}
			if k == reflect.Int {
				*(ptr.(*int)) = i
			} else if k == reflect.Int64 {
				*(ptr.(*int64)) = int64(i)
			}
			return found, nil
		case reflect.Float64:
			f, found, err := o.obj.GetNestedFloat(fields...)
			if err != nil || !found {
				return found, err
			}
			*(ptr.(*float64)) = f
			return found, nil
		case reflect.Bool:
			b, found, err := o.obj.GetNestedBool(fields...)
			if err != nil || !found {
				return found, err
			}
			*(ptr.(*bool)) = b
			return found, nil
		default:
			return false, fmt.Errorf("unhandled kind %s", k)
		}
	}()
	if err != nil {
		return found, fmt.Errorf("unable to get fields %v as %T with error: %w", fields, ptr, err)
	}
	return found, nil
}

// SetOrDie sets a nested field located by fields to the value provided as val.
// It will panic if it encounters any error.
func (o *SubObject) SetOrDie(val interface{}, fields ...string) {
	if err := o.SetNestedField(val, fields...); err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// onLockedFields locks the SubObject fields which are expected for kpt internal use only.
func (o *SubObject) onLockedFields(val interface{}, fields ...string) error {
	if o.hasUpstreamIdentifier(val, fields...) {
		return ErrAttemptToTouchUpstreamIdentifier{}
	}
	return nil
}

// SetNestedField sets a nested field located by fields to the value provided as val. val
// should not be a yaml.RNode. If you want to deal with yaml.RNode, you should
// use Get method and modify the underlying yaml.Node.
func (o *SubObject) SetNestedField(val interface{}, fields ...string) error {
	if err := o.onLockedFields(val, fields...); err != nil {
		return err
	}
	err := func() error {
		if val == nil {
			return fmt.Errorf("the passed-in object must not be nil")
		}
		if o == nil {
			return fmt.Errorf("the object doesn't exist")
		}
		if o.obj == nil {
			o.obj = internal.NewMap(nil)
		}
		kind := reflect.ValueOf(val).Kind()
		if kind == reflect.Ptr {
			kind = reflect.TypeOf(val).Elem().Kind()
		}

		switch kind {
		case reflect.Struct, reflect.Map:
			m, err := internal.TypedObjectToMapVariant(val)
			if err != nil {
				return err
			}
			return o.obj.SetNestedMap(m, fields...)
		case reflect.Slice:
			s, err := internal.TypedObjectToSliceVariant(val)
			if err != nil {
				return err
			}
			return o.obj.SetNestedSlice(s, fields...)
		case reflect.String:
			var s string
			switch val := val.(type) {
			case string:
				s = val
			case *string:
				s = *val
			}
			return o.obj.SetNestedString(s, fields...)
		case reflect.Int, reflect.Int64:
			var i int
			switch val := val.(type) {
			case int:
				i = val
			case *int:
				i = *val
			case int64:
				i = int(val)
			case *int64:
				i = int(*val)
			}
			return o.obj.SetNestedInt(i, fields...)
		case reflect.Float64:
			var f float64
			switch val := val.(type) {
			case float64:
				f = val
			case *float64:
				f = *val
			}
			return o.obj.SetNestedFloat(f, fields...)
		case reflect.Bool:
			var b bool
			switch val := val.(type) {
			case bool:
				b = val
			case *bool:
				b = *val
			}
			return o.obj.SetNestedBool(b, fields...)
		default:
			return fmt.Errorf("unhandled kind %s", kind)
		}
	}()
	if err != nil {
		return fmt.Errorf("unable to set %v at fields %v with error: %w", val, fields, err)
	}
	return nil
}

// SetNestedIntOrDie sets the `fields` value to int `value`. It panics if the fields type is not int.
func (o *SubObject) SetNestedIntOrDie(value int, fields ...string) {
	err := o.SetNestedInt(value, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// SetNestedInt sets the `fields` value to int `value`. It returns error if the fields type is not int.
func (o *SubObject) SetNestedInt(value int, fields ...string) error {
	return o.SetNestedField(value, fields...)
}

// SetNestedBoolOrDie sets the `fields` value to bool `value`. It panics if the fields type is not bool.
func (o *SubObject) SetNestedBoolOrDie(value bool, fields ...string) {
	err := o.SetNestedBool(value, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// SetNestedBool sets the `fields` value to bool `value`. It returns error if the fields type is not bool.
func (o *SubObject) SetNestedBool(value bool, fields ...string) error {
	return o.SetNestedField(value, fields...)
}

// SetNestedStringOrDie sets the `fields` value to string `value`. It panics if the fields type is not string.
func (o *SubObject) SetNestedStringOrDie(value string, fields ...string) {
	err := o.SetNestedString(value, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// SetNestedStringOrDie sets the `fields` value to string `value`. It returns error if the fields type is not string.
func (o *SubObject) SetNestedString(value string, fields ...string) error {
	return o.SetNestedField(value, fields...)
}

// SetNestedStringMapOrDie sets the `fields` value to map[string]string `value`. It panics if the fields type is not map[string]string.
func (o *SubObject) SetNestedStringMapOrDie(value map[string]string, fields ...string) {
	err := o.SetNestedStringMap(value, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// SetNestedStringMap sets the `fields` value to map[string]string `value`. It returns error if the fields type is not map[string]string.
func (o *SubObject) SetNestedStringMap(value map[string]string, fields ...string) error {
	return o.SetNestedField(value, fields...)
}

// SetNestedStringSliceOrDie sets the `fields` value to []string `value`. It panics if the fields type is not []string.
func (o *SubObject) SetNestedStringSliceOrDie(value []string, fields ...string) {
	err := o.SetNestedStringSlice(value, fields...)
	if err != nil {
		panic(errSubObjectFields{fields: fields})
	}
}

// SetNestedStringSlice sets the `fields` value to []string `value`. It returns error if the fields type is not []string.
func (o *SubObject) SetNestedStringSlice(value []string, fields ...string) error {
	return o.SetNestedField(value, fields...)
}

// LineComment returns the line comment, if the target field exist and a
// potential error.
func (o *KubeObject) LineComment(fields ...string) (string, bool, error) {
	rn := &yaml.RNode{}
	found, err := o.Get(rn, fields...)
	if !found || err != nil {
		return "", found, err
	}
	return rn.YNode().LineComment, true, nil
}

// HeadComment returns the head comment, if the target field exist and a
// potential error.
func (o *KubeObject) HeadComment(fields ...string) (string, bool, error) {
	rn := &yaml.RNode{}
	found, err := o.Get(rn, fields...)
	if !found || err != nil {
		return "", found, err
	}
	return rn.YNode().HeadComment, true, nil
}

func (o *KubeObject) SetLineComment(comment string, fields ...string) error {
	rn := &yaml.RNode{}
	found, err := o.Get(rn, fields...)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("can't set line comment because the field doesn't exist")
	}
	rn.YNode().LineComment = comment
	return nil
}

func (o *KubeObject) SetHeadComment(comment string, fields ...string) error {
	rn := &yaml.RNode{}
	found, err := o.Get(rn, fields...)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("can't set head comment because the field doesn't exist")
	}
	rn.YNode().HeadComment = comment
	return nil
}

// AsOrDie converts a KubeObject to the desired typed object. ptr must
// be a pointer to a typed object. It will panic if it encounters an error.
func (o *SubObject) AsOrDie(ptr interface{}) {
	if err := o.As(ptr); err != nil {
		panic(errSubObjectFields{fields: nil})
	}
}

// As converts a KubeObject to the desired typed object. ptr must be
// a pointer to a typed object.
func (o *SubObject) As(ptr interface{}) error {
	err := func() error {
		if o == nil {
			return fmt.Errorf("the object doesn't exist")
		}
		if ptr == nil || reflect.ValueOf(ptr).Kind() != reflect.Ptr {
			return fmt.Errorf("ptr must be a pointer to an object")
		}
		return internal.MapVariantToTypedObject(o.obj, ptr)
	}()
	if err != nil {
		return fmt.Errorf("unable to convert object to %T with error: %w", ptr, err)
	}
	return nil
}

// NewFromTypedObject construct a KubeObject from a typed object (e.g. corev1.Pod)
func NewFromTypedObject(v interface{}) (*KubeObject, error) {
	m, err := internal.TypedObjectToMapVariant(v)
	if err != nil {
		return nil, err
	}
	return asKubeObject(m), nil
}

// String serializes the object in yaml format.
func (o *KubeObject) String() string {
	doc := internal.NewDoc([]*yaml.Node{o.obj.Node()}...)
	s, _ := doc.ToYAML()
	return string(s)
}

// ShortString provides a human readable information for the KubeObject Identifier in the form of GVKNN.
func (o *KubeObject) ShortString() string {
	return fmt.Sprintf("Resource(apiVersion=%v, kind=%v, namespace=%v, name=%v)",
		o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())
}

// resourceIdentifier returns the resource identifier including apiVersion, kind,
// namespace and name.
func (o *KubeObject) resourceIdentifier() *yaml.ResourceIdentifier {
	apiVersion := o.GetAPIVersion()
	kind := o.GetKind()
	name := o.GetName()
	ns := o.GetNamespace()
	return &yaml.ResourceIdentifier{
		TypeMeta: yaml.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		NameMeta: yaml.NameMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}

// IsGVK compares the given group, version, and kind with KubeObject's apiVersion and Kind.
func (o *KubeObject) IsGVK(group, version, kind string) bool {
	var apiVersion string
	switch {
	case group == "":
		apiVersion = version
	default:
		apiVersion = group + "/" + version
	}

	if o.GetAPIVersion() == apiVersion && o.GetKind() == kind {
		return true
	}
	if apiVersion == "" && o.GetKind() == kind {
		return true
	}
	if kind == "" && o.GetAPIVersion() == apiVersion {
		return true
	}
	return false
}

// IsLocalConfig checks the "config.kubernetes.io/local-config" field to tell
// whether a KRM resource will be skipped by `kpt live apply` or not.
func (o *KubeObject) IsLocalConfig() bool {
	isLocalConfig := o.GetAnnotation(KptLocalConfig)
	if isLocalConfig == "" || isLocalConfig == "false" {
		return false
	}
	return true
}

func (o *KubeObject) GetAPIVersion() string {
	apiVersion, _, _ := o.obj.GetNestedString("apiVersion")
	return apiVersion
}

func (o *KubeObject) SetAPIVersion(apiVersion string) {
	if err := o.obj.SetNestedString(apiVersion, "apiVersion"); err != nil {
		panic(fmt.Errorf("cannot set apiVersion '%v': %v", apiVersion, err))
	}
}

func (o *KubeObject) GetKind() string {
	kind, _, _ := o.obj.GetNestedString("kind")
	return kind
}

func (o *KubeObject) SetKind(kind string) {
	if err := o.SetNestedField(kind, "kind"); err != nil {
		panic(fmt.Errorf("cannot set kind '%v': %v", kind, err))
	}
}

func (o *KubeObject) GetName() string {
	s, _, _ := o.obj.GetNestedString("metadata", "name")
	return s
}

func (o *KubeObject) SetName(name string) {
	if err := o.SetNestedField(name, "metadata", "name"); err != nil {
		panic(fmt.Errorf("cannot set metadata name '%v': %v", name, err))
	}
}

func (o *KubeObject) GetNamespace() string {
	s, _, _ := o.obj.GetNestedString("metadata", "namespace")
	return s
}

// IsNamespaceScoped tells whether a k8s resource is namespace scoped. If the KubeObject resource is a customized, it
// determines the namespace scope by checking whether `metadata.namespace` is set.
func (o *KubeObject) IsNamespaceScoped() bool {
	tm := yaml.TypeMeta{Kind: o.GetKind(), APIVersion: o.GetAPIVersion()}
	if nsScoped, ok := internal.PrecomputedIsNamespaceScoped[tm]; ok {
		return nsScoped
	}
	// TODO(yuwenma): parse the resource openapi schema to know its scope status.
	return o.HasNamespace()
}

// IsClusterScoped tells whether a resource is cluster scoped.
func (o *KubeObject) IsClusterScoped() bool {
	return !o.IsNamespaceScoped()
}

func (o *KubeObject) HasNamespace() bool {
	_, found, _ := o.obj.GetNestedString("metadata", "namespace")
	return found
}

func (o *KubeObject) SetNamespace(name string) {
	if err := o.SetNestedField(name, "metadata", "namespace"); err != nil {
		panic(fmt.Errorf("cannot set namespace '%v': %v", name, err))
	}
}

func (o *KubeObject) SetAnnotation(k, v string) {
	// Keep upstream-identifier untouched from users
	if k == UpstreamIdentifier {
		panic(ErrAttemptToTouchUpstreamIdentifier{})
	}
	if err := o.SetNestedField(v, "metadata", "annotations", k); err != nil {
		panic(fmt.Errorf("cannot set metadata annotations '%v': %v", k, err))
	}
}

// GetAnnotations returns all annotations.
func (o *KubeObject) GetAnnotations() map[string]string {
	v, _, _ := o.obj.GetNestedStringMap("metadata", "annotations")
	return v
}

// GetAnnotation returns one annotation with key k.
func (o *KubeObject) GetAnnotation(k string) string {
	v, _, _ := o.obj.GetNestedString("metadata", "annotations", k)
	return v
}

// HasAnnotations returns whether the KubeObject has all the given annotations.
func (o *KubeObject) HasAnnotations(annotations map[string]string) bool {
	kubeObjectLabels := o.GetAnnotations()
	for k, v := range annotations {
		kubeObjectValue, found := kubeObjectLabels[k]
		if !found || kubeObjectValue != v {
			return false
		}
	}
	return true
}

// RemoveAnnotationsIfEmpty removes the annotations field when it has zero annotations.
func (o *KubeObject) RemoveAnnotationsIfEmpty() error {
	annotations, found, err := o.obj.GetNestedStringMap("metadata", "annotations")
	if err != nil {
		return err
	}
	if found && len(annotations) == 0 {
		_, err = o.obj.RemoveNestedField("metadata", "annotations")
		return err
	}
	return nil
}

func (o *KubeObject) SetLabel(k, v string) {
	if err := o.SetNestedField(v, "metadata", "labels", k); err != nil {
		panic(fmt.Errorf("cannot set metadata labels '%v': %v", k, err))
	}
}

// Label returns one label with key k.
func (o *KubeObject) GetLabel(k string) string {
	v, _, _ := o.obj.GetNestedString("metadata", "labels", k)
	return v
}

// Labels returns all labels.
func (o *KubeObject) GetLabels() map[string]string {
	v, _, _ := o.obj.GetNestedStringMap("metadata", "labels")
	return v
}

// HasLabels returns whether the KubeObject has all the given labels
func (o *KubeObject) HasLabels(labels map[string]string) bool {
	kubeObjectLabels := o.GetLabels()
	for k, v := range labels {
		kubeObjectValue, found := kubeObjectLabels[k]
		if !found || kubeObjectValue != v {
			return false
		}
	}
	return true
}

func (o *KubeObject) PathAnnotation() string {
	anno := o.GetAnnotation(kioutil.PathAnnotation)
	return anno
}

// IndexAnnotation return -1 if not found.
func (o *KubeObject) IndexAnnotation() int {
	anno := o.GetAnnotation(kioutil.IndexAnnotation)
	if anno == "" {
		return -1
	}
	i, _ := strconv.Atoi(anno)
	return i
}

// IdAnnotation return -1 if not found.
func (o *KubeObject) IdAnnotation() int {
	anno := o.GetAnnotation(kioutil.IdAnnotation)

	if anno == "" {
		return -1
	}
	i, _ := strconv.Atoi(anno)
	return i
}

type KubeObjects []*KubeObject

func (o KubeObjects) Len() int      { return len(o) }
func (o KubeObjects) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o KubeObjects) Less(i, j int) bool {
	idi := o[i].resourceIdentifier()
	idj := o[j].resourceIdentifier()
	idStrI := fmt.Sprintf("%s %s %s %s", idi.GetAPIVersion(), idi.GetKind(), idi.GetNamespace(), idi.GetName())
	idStrJ := fmt.Sprintf("%s %s %s %s", idj.GetAPIVersion(), idj.GetKind(), idj.GetNamespace(), idj.GetName())
	return idStrI < idStrJ
}

func (o KubeObjects) String() string {
	var elems []string
	for _, obj := range o {
		elems = append(elems, strings.TrimSpace(obj.String()))
	}
	return strings.Join(elems, "\n---\n")
}

// Where will return the subset of objects in KubeObjects such that f(object) returns 'true'.
func (o KubeObjects) Where(f func(*KubeObject) bool) KubeObjects {
	var result KubeObjects
	for _, obj := range o {
		if f(obj) {
			result = append(result, obj)
		}
	}
	return result
}

// Not returns will return a function that returns the opposite of f(object), i.e. !f(object)
func Not(f func(*KubeObject) bool) func(o *KubeObject) bool {
	return func(o *KubeObject) bool {
		return !f(o)
	}
}

// WhereNot will return the subset of objects in KubeObjects such that f(object) returns 'false'.
// This is a shortcut for Where(Not(f)).
func (o KubeObjects) WhereNot(f func(o *KubeObject) bool) KubeObjects {
	return o.Where(Not(f))
}

// IsGVK returns a function that checks if a KubeObject has a certain GVK.
func IsGVK(group, version, kind string) func(*KubeObject) bool {
	return func(o *KubeObject) bool {
		return o.IsGVK(group, version, kind)
	}
}

// IsName returns a function that checks if a KubeObject has a certain name.
func IsName(name string) func(*KubeObject) bool {
	return func(o *KubeObject) bool {
		return o.GetName() == name
	}
}

// IsNamespace returns a function that checks if a KubeObject has a certain namespace.
func IsNamespace(namespace string) func(*KubeObject) bool {
	return func(o *KubeObject) bool {
		return o.GetNamespace() == namespace
	}
}

// HasLabels returns a function that checks if a KubeObject has all the given labels.
func HasLabels(labels map[string]string) func(*KubeObject) bool {
	return func(o *KubeObject) bool {
		return o.HasLabels(labels)
	}
}

// HasAnnotations returns a function that checks if a KubeObject has all the given annotations.
func HasAnnotations(annotations map[string]string) func(*KubeObject) bool {
	return func(o *KubeObject) bool {
		return o.HasAnnotations(annotations)
	}
}

// IsMetaResource returns a function that checks if a KubeObject is a meta resource. For now
// this just includes the Kptfile
func IsMetaResource() func(*KubeObject) bool {
	return IsGVK("kpt.dev", "v1", "Kptfile")
}

func (o *KubeObject) IsEmpty() bool {
	return yaml.IsYNodeEmptyMap(o.obj.Node())
}

func NewEmptyKubeObject() *KubeObject {
	return &KubeObject{SubObject{internal.NewMap(nil)}}
}

func asKubeObject(obj *internal.MapVariant) *KubeObject {
	return &KubeObject{SubObject{obj}}
}

func (o *KubeObject) node() *internal.MapVariant {
	return o.obj
}

func rnodeToKubeObject(rn *yaml.RNode) *KubeObject {
	mapVariant := internal.NewMap(rn.YNode())
	return &KubeObject{SubObject{mapVariant}}
}

// SubObject represents a map within a KubeObject
type SubObject struct {
	obj *internal.MapVariant
}

func (o *SubObject) UpsertMap(k string) *SubObject {
	m := o.obj.UpsertMap(k)
	return &SubObject{obj: m}
}

// GetMap accepts a single key `k` whose value is expected to be a map. It returns
// the map in the form of a SubObject pointer.
// It panic with ErrSubObjectFields error if the field cannot be represented as a SubObject.
func (o *SubObject) GetMap(k string) *SubObject {
	var variant yaml.RNode
	found, err := o.Get(&variant, k)
	if err != nil || !found {
		return nil
	}
	return &SubObject{obj: internal.NewMap(variant.YNode())}
}

// GetBool accepts a single key `k` whose value is expected to be a boolean. It returns
// the int value of the `k`. It panic with errSubObjectFields error if the
// field is not an integer type.
func (o *SubObject) GetBool(k string) bool {
	return o.NestedBoolOrDie(k)
}

// GetInt accepts a single key `k` whose value is expected to be an integer. It returns
// the int value of the `k`. It panic with errSubObjectFields error if the
// field is not an integer type.
func (o *SubObject) GetInt(k string) int64 {
	return o.NestedInt64OrDie(k)
}

// GetString accepts a single key `k` whose value is expected to be a string. It returns
// the value of the `k`. It panic with errSubObjectFields error if the
// field is not a string type.
func (o *SubObject) GetString(k string) string {
	return o.NestedStringOrDie(k)
}

// GetSlice accepts a single key `k` whose value is expected to be a slice. It returns
// the value as a slice of SubObject. It panic with errSubObjectFields error if the
// field is not a slice type.
func (o *SubObject) GetSlice(k string) SliceSubObjects {
	return o.NestedSliceOrDie(k)
}

type SliceSubObjects []*SubObject

// MarshalJSON provides the custom encoding format for encode.json. This is used
// when KubeObject `Set` a slice of SubObjects.
func (s *SliceSubObjects) MarshalJSON() ([]byte, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for _, subObject := range *s {
		node.Content = append(node.Content, subObject.obj.Node())
	}
	return yaml.NewRNode(node).MarshalJSON()
}
