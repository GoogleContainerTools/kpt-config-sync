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

package nomostest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Predicate evaluates a client.Object, returning an error if it fails validation.
type Predicate func(o client.Object) error

// ErrWrongType indicates that the caller passed an object of the incorrect type
// to the Predicate.
var ErrWrongType = errors.New("wrong type")

// WrongTypeErr reports that the passed type was not equivalent to the wanted
// type.
func WrongTypeErr(got, want interface{}) error {
	return fmt.Errorf("%w: got %T, want %T", ErrWrongType, got, want)
}

// ErrFailedPredicate indicates the the object on the API server does not match
// the Predicate.
var ErrFailedPredicate = errors.New("failed predicate")

// HasAnnotation returns a predicate that tests if an Object has the specified
// annotation key/value pair.
func HasAnnotation(key, value string) Predicate {
	return func(o client.Object) error {
		got, ok := o.GetAnnotations()[key]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q; want %q", o.GetName(), key, value)
		}
		if got != value {
			return fmt.Errorf("got %q for annotation %q on object %q; want %q", got, key, o.GetName(), value)
		}
		return nil
	}
}

// HasAnnotationKey returns a predicate that tests if an Object has the specified
// annotation key.
func HasAnnotationKey(key string) Predicate {
	return func(o client.Object) error {
		_, ok := o.GetAnnotations()[key]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), key)
		}
		return nil
	}
}

// HasAllAnnotationKeys returns a predicate that tests if an Object has the specified
// annotation keys.
func HasAllAnnotationKeys(keys ...string) Predicate {
	return func(o client.Object) error {
		for _, key := range keys {
			predicate := HasAnnotationKey(key)

			err := predicate(o)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// MissingAnnotation returns a predicate that tests that an object does not have
// a specified annotation.
func MissingAnnotation(key string) Predicate {
	return func(o client.Object) error {
		_, ok := o.GetAnnotations()[key]
		if ok {
			return fmt.Errorf("object %v has annotation %s, want missing", o.GetName(), key)
		}
		return nil
	}
}

// HasLabel returns a predicate that tests if an Object has the specified label key/value pair.
func HasLabel(key, value string) Predicate {
	return func(o client.Object) error {
		got, ok := o.GetLabels()[key]
		if !ok {
			return fmt.Errorf("object %q does not have label %q; wanted %q", o.GetName(), key, value)
		}
		if got != value {
			return fmt.Errorf("got %q for label %q on object %q; wanted %q", got, key, o.GetName(), value)
		}
		return nil
	}
}

// MissingLabel returns a predicate that tests that an object does not have
// a specified label.
func MissingLabel(key string) Predicate {
	return func(o client.Object) error {
		_, ok := o.GetLabels()[key]
		if ok {
			return fmt.Errorf("object %v has label %s, want missing", o.GetName(), key)
		}
		return nil
	}
}

// HasExactlyAnnotationKeys ensures the Object has exactly the passed set of
// annotations, ignoring values.
func HasExactlyAnnotationKeys(wantKeys ...string) Predicate {
	sort.Strings(wantKeys)
	return func(o client.Object) error {
		annotations := o.GetAnnotations()
		var gotKeys []string
		for k := range annotations {
			gotKeys = append(gotKeys, k)
		}
		sort.Strings(gotKeys)
		if diff := cmp.Diff(wantKeys, gotKeys); diff != "" {
			return errors.Errorf("unexpected diff in metadata.annotation keys: %s", diff)
		}
		return nil
	}
}

// HasExactlyLabelKeys ensures the Object has exactly the passed set of
// labels, ignoring values.
func HasExactlyLabelKeys(wantKeys ...string) Predicate {
	wantKeys = append(wantKeys, TestLabel)
	sort.Strings(wantKeys)
	return func(o client.Object) error {
		labels := o.GetLabels()
		var gotKeys []string
		for k := range labels {
			gotKeys = append(gotKeys, k)
		}
		sort.Strings(gotKeys)
		if diff := cmp.Diff(wantKeys, gotKeys); diff != "" {
			return errors.Errorf("unexpected diff in metadata.annotation keys: %s", diff)
		}
		return nil
	}
}

//HasExactlyImageName ensures a container has the expected image name
func HasExactlyImageName(containerName string, expectImageName string) Predicate {
	return func(o client.Object) error {
		dep, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(dep, &appsv1.Deployment{})
		}
		for _, container := range dep.Spec.Template.Spec.Containers {
			if containerName == container.Name {
				if !strings.Contains(container.Image, "/"+expectImageName+":") {
					return errors.Errorf(" Expected %q container image name is %q, however the actual image is %q", container.Name, expectImageName, container.Image)
				}
				return nil
			}
		}
		return errors.Errorf("Container %q not found", containerName)
	}

}

// HasCorrectResourceRequestsLimits verify a root/namespace reconciler container has the correct resource requests and limits.
func HasCorrectResourceRequestsLimits(containerName string, cpuRequest, cpuLimit, memoryRequest, memoryLimit resource.Quantity) Predicate {
	return func(o client.Object) error {
		dep, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(dep, &appsv1.Deployment{})
		}
		for _, container := range dep.Spec.Template.Spec.Containers {
			if containerName == container.Name {
				if container.Resources.Requests[corev1.ResourceCPU] != cpuRequest {
					return errors.Errorf("The CPU request of the %q container should be %v, got %v", container.Name, cpuRequest, container.Resources.Requests[corev1.ResourceCPU])
				}
				if container.Resources.Limits[corev1.ResourceCPU] != cpuLimit {
					return errors.Errorf("The CPU limit of the %q container should be %v, got %v", container.Name, cpuLimit, container.Resources.Limits[corev1.ResourceCPU])
				}
				if container.Resources.Requests[corev1.ResourceMemory] != memoryRequest {
					return errors.Errorf("The memory request of the %q container should be %v, got %v", container.Name, memoryRequest, container.Resources.Requests[corev1.ResourceMemory])
				}
				if container.Resources.Limits[corev1.ResourceMemory] != memoryLimit {
					return errors.Errorf("The memory limit of the %q container should be %v, got %v", container.Name, memoryLimit, container.Resources.Limits[corev1.ResourceMemory])
				}

				return nil
			}
		}
		return errors.Errorf("Container %q not found", containerName)
	}
}

// NotPendingDeletion ensures o is not pending deletion.
//
// Check this when the object could be scheduled for deletion, to avoid flaky
// behavior when we're ensuring we don't want something to be deleted.
func NotPendingDeletion(o client.Object) error {
	if o.GetDeletionTimestamp() == nil {
		return nil
	}
	return errors.Errorf("object has non-nil deletionTimestamp")
}

// HasAllNomosMetadata ensures that the object contains the expected
// nomos labels and annotations.
func HasAllNomosMetadata(multiRepo bool) Predicate {
	return func(o client.Object) error {
		annotationKeys := metadata.GetNomosAnnotationKeys(multiRepo)
		labels := metadata.SyncerLabels()

		predicates := []Predicate{HasAllAnnotationKeys(annotationKeys...), HasAnnotation("configmanagement.gke.io/managed", "enabled")}
		for labelKey, value := range labels {
			predicates = append(predicates, HasLabel(labelKey, value))
		}

		for _, predicate := range predicates {
			err := predicate(o)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

// NoConfigSyncMetadata ensures that the object doesn't
// contain configsync labels and annotations.
func NoConfigSyncMetadata() Predicate {
	return func(o client.Object) error {
		if metadata.HasConfigSyncMetadata(o) {
			return fmt.Errorf("object %q shouldn't have configsync metadta %v, %v", o.GetName(), o.GetLabels(), o.GetAnnotations())
		}
		return nil
	}
}

// AllResourcesAreCurrent ensures that the managed resources
// are all Current in the ResourceGroup CR.
func AllResourcesAreCurrent() Predicate {
	return func(o client.Object) error {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			return WrongTypeErr(u, &unstructured.Unstructured{})
		}
		resourceStatuses, found, err := unstructured.NestedSlice(u.Object, "status", "resourceStatuses")
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("resource status not found in %v", u)
		}
		for _, resource := range resourceStatuses {
			s, ok := resource.(map[string]interface{})
			if !ok {
				return WrongTypeErr(s, map[string]interface{}{})
			}
			status, found, err := unstructured.NestedString(s, "status")
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("status field not found for resource %v", resource)
			}
			if status != "Current" {
				return fmt.Errorf("status %v is not Current", status)
			}
		}
		return nil
	}
}

// HasStatus checks that the ResourceGroup object
// has a non empty status field.
func HasStatus() Predicate {
	return func(o client.Object) error {
		hasStatus, err := hasStatus(o)
		if err != nil {
			return err
		}
		if hasStatus {
			return nil
		}
		return fmt.Errorf("not found status in %v", o)
	}
}

// NoStatus checks that the ResourceGroup object
// has an empty status field.
func NoStatus() Predicate {
	return func(o client.Object) error {
		hasStatus, err := hasStatus(o)
		if err != nil {
			return err
		}
		if hasStatus {
			return fmt.Errorf("found non empty status in %v", o)
		}
		return nil
	}
}

func hasStatus(o client.Object) (bool, error) {
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return false, WrongTypeErr(u, &unstructured.Unstructured{})
	}
	status, found, err := unstructured.NestedMap(u.Object, "status")
	if err != nil {
		return false, err
	}
	if !found || len(status) <= 1 {
		// By default, it contains the field .status.observedGeneration: 0.
		return false, nil
	}
	return true, nil
}

// DeploymentHasEnvVar check whether the deployment contains environment variable
// with specified name and value
func DeploymentHasEnvVar(containerName, key, value string) Predicate {

	return func(o client.Object) error {
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(o, d)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				for _, e := range c.Env {
					if e.Name == key {
						if e.Value == value {
							return nil
						}
						return errors.Errorf("Container %q has the wrong value for environment variable %q. Expected : %q, actual %q", containerName, key, value, e.Value)
					}
				}
			}
		}

		return errors.Errorf("Container %q does not contain environment variable %q", containerName, key)
	}
}

// DeploymentMissingEnvVar check whether the deployment does not contain environment variable
// with specified name and value
func DeploymentMissingEnvVar(containerName, key string) Predicate {

	return func(o client.Object) error {
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(o, d)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				for _, e := range c.Env {
					if e.Name == key {
						return errors.Errorf("Container %q should not have environment variable %q", containerName, key)
					}
				}
			}
		}

		return nil
	}
}

// IsManagedBy checks that the object is managed by configsync, has the expected
// resource manager, and has a valid resource-id.
// Use diff.IsManager if you just need a boolean without errors.
func IsManagedBy(nt *NT, scope declared.Scope, syncName string) Predicate {
	return func(obj client.Object) error {
		// Make sure GVK is populated (it's usually not for typed structs).
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = apiutil.GVKForObject(obj, nt.scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
		}

		// managed is required by differ.ManagedByConfigSync
		managedValue := core.GetAnnotation(obj, metadata.ResourceManagementKey)
		if managedValue != metadata.ResourceManagementEnabled {
			return errors.Errorf("expected %s %s to be managed by configsync, but found %q=%q",
				gvk.Kind, ObjectNamespacedName(obj),
				metadata.ResourceManagementKey, managedValue)
		}

		// manager is required by diff.IsManager
		expectedManager := declared.ResourceManager(scope, syncName)
		managerValue := core.GetAnnotation(obj, metadata.ResourceManagerKey)
		if managerValue != expectedManager {
			return errors.Errorf("expected %s %s to be managed by %q, but found %q=%q",
				gvk.Kind, ObjectNamespacedName(obj),
				expectedManager, metadata.ResourceManagerKey, managerValue)
		}

		// resource-id is required by differ.ManagedByConfigSync
		expectedID := core.GKNN(obj)
		resourceIDValue := core.GetAnnotation(obj, metadata.ResourceIDKey)
		if resourceIDValue != expectedID {
			return errors.Errorf("expected %s %s to have resource-id %q, but found %q=%q",
				gvk.Kind, ObjectNamespacedName(obj),
				expectedID, metadata.ResourceIDKey, resourceIDValue)
		}
		return nil
	}
}

// IsNotManaged checks that the object is NOT managed by configsync.
// Use differ.ManagedByConfigSync if you just need a boolean without errors.
func IsNotManaged(nt *NT) Predicate {
	return func(obj client.Object) error {
		// Make sure GVK is populated (it's usually not for typed structs).
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = apiutil.GVKForObject(obj, nt.scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
		}

		// manager is required by diff.IsManager.
		managerValue := core.GetAnnotation(obj, metadata.ResourceManagerKey)
		if managerValue != "" {
			return errors.Errorf("expected %s %s to NOT have a manager, but found %q=%q",
				gvk.Kind, ObjectNamespacedName(obj),
				metadata.ResourceManagerKey, managerValue)
		}

		// managed is required by differ.ManagedByConfigSync
		managedValue := core.GetAnnotation(obj, metadata.ResourceManagementKey)
		if managedValue == metadata.ResourceManagementEnabled {
			return errors.Errorf("expected %s %s to NOT have management enabled, but found %q=%q",
				gvk.Kind, ObjectNamespacedName(obj),
				metadata.ResourceManagementKey, managedValue)
		}
		return nil
	}
}

// ResourceVersionEquals checks that the object's ResourceVersion matches the
// specified value.
func ResourceVersionEquals(nt *NT, expected string) Predicate {
	return func(obj client.Object) error {
		resourceVersion := obj.GetResourceVersion()
		if resourceVersion == expected {
			return nil
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = apiutil.GVKForObject(obj, nt.scheme)
			if err != nil {
				return err
			}
		}
		return errors.Errorf("expected %s %s to have resourceVersion %q, but got %q",
			gvk.Kind, ObjectNamespacedName(obj),
			expected, resourceVersion)
	}
}

// ResourceVersionNotEquals checks that the object's ResourceVersion does NOT
// match specified value.
func ResourceVersionNotEquals(nt *NT, unexpected string) Predicate {
	return func(obj client.Object) error {
		resourceVersion := obj.GetResourceVersion()
		if resourceVersion != unexpected {
			return nil
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = apiutil.GVKForObject(obj, nt.scheme)
			if err != nil {
				return err
			}
		}
		return errors.Errorf("expected %s %s to NOT have resourceVersion %q, but got %q",
			gvk.Kind, ObjectNamespacedName(obj),
			unexpected, resourceVersion)
	}
}

// StatusEquals checks that the object's computed status matches the specified
// status.
func StatusEquals(nt *NT, expected status.Status) Predicate {
	return func(obj client.Object) error {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = apiutil.GVKForObject(obj, nt.scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
		}

		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return errors.Wrapf(err, "failed to convert %s %s to unstructured",
				gvk.Kind, ObjectNamespacedName(obj))
		}
		uObj := &unstructured.Unstructured{Object: uMap}

		result, err := status.Compute(uObj)
		if err != nil {
			return errors.Wrapf(err, "failed to compute status for %s %s",
				gvk.Kind, ObjectNamespacedName(obj))
		}

		if result.Status != expected {
			return errors.Errorf("expected %s %s to have status %q, but got %q",
				gvk.Kind, ObjectNamespacedName(obj),
				expected, result.Status)
		}
		return nil
	}
}
