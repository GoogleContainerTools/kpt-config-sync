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

package testpredicates

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Predicate evaluates a client.Object, returning an error if it fails validation.
// The object will be nil if the object was deleted or never existed.
type Predicate func(o client.Object) error

// ErrWrongType indicates that the caller passed an object of the incorrect type
// to the Predicate.
var ErrWrongType = errors.New("wrong type")

// ErrObjectNotFound indicates that the caller passed a nil object, indicating
// the object was deleted or never existed.
var ErrObjectNotFound = errors.New("object not found")

// WrongTypeErr reports that the passed type was not equivalent to the wanted
// type.
func WrongTypeErr(got, want interface{}) error {
	return retry.NewTerminalError(
		fmt.Errorf("%w: got %T, want %T", ErrWrongType, got, want))
}

// ErrFailedPredicate indicates the the object on the API server does not match
// the Predicate.
var ErrFailedPredicate = errors.New("failed predicate")

// EvaluatePredicates evaluates a list of predicates and returns any errors
func EvaluatePredicates(obj client.Object, predicates []Predicate) []error {
	var errs []error
	for _, predicate := range predicates {
		if err := predicate(obj); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// HasAnnotation returns a predicate that tests if an Object has the specified
// annotation key/value pair.
func HasAnnotation(key, value string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
	wantKeys = append(wantKeys, testkubeclient.TestLabel)
	sort.Strings(wantKeys)
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
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

// HasExactlyImage ensures a container has the expected image.
func HasExactlyImage(containerName, expectImageName, expectImageTag, expectImageDigest string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		dep, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(dep, &appsv1.Deployment{})
		}
		for _, container := range dep.Spec.Template.Spec.Containers {
			if containerName == container.Name {
				expectImage := ""
				if expectImageName != "" {
					expectImage = "/" + expectImageName
				}
				if expectImageTag != "" {
					expectImage += ":" + expectImageTag
				}
				if expectImageDigest != "" {
					expectImage += "@" + expectImageDigest
				}
				if !strings.Contains(container.Image, expectImage) {
					return errors.Errorf("Expected %q container image contains %q, however the actual image is %q", container.Name, expectImage, container.Image)
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
		if o == nil {
			return ErrObjectNotFound
		}
		dep, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(dep, &appsv1.Deployment{})
		}
		for _, container := range dep.Spec.Template.Spec.Containers {
			if containerName == container.Name {
				if !equality.Semantic.DeepEqual(container.Resources.Requests[corev1.ResourceCPU], cpuRequest) {
					return errors.Errorf("The CPU request of the %q container should be %v, got %v", container.Name, cpuRequest, container.Resources.Requests[corev1.ResourceCPU])
				}
				if !equality.Semantic.DeepEqual(container.Resources.Limits[corev1.ResourceCPU], cpuLimit) {
					return errors.Errorf("The CPU limit of the %q container should be %v, got %v", container.Name, cpuLimit, container.Resources.Limits[corev1.ResourceCPU])
				}
				if !equality.Semantic.DeepEqual(container.Resources.Requests[corev1.ResourceMemory], memoryRequest) {
					return errors.Errorf("The memory request of the %q container should be %v, got %v", container.Name, memoryRequest, container.Resources.Requests[corev1.ResourceMemory])
				}
				if !equality.Semantic.DeepEqual(container.Resources.Limits[corev1.ResourceMemory], memoryLimit) {
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
	if o == nil {
		return ErrObjectNotFound
	}
	if o.GetDeletionTimestamp() == nil {
		return nil
	}
	return errors.Errorf("object has non-nil deletionTimestamp")
}

// HasAllNomosMetadata ensures that the object contains the expected
// nomos labels and annotations.
func HasAllNomosMetadata() Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		annotationKeys := metadata.GetNomosAnnotationKeys()
		labels := metadata.SyncerLabels()
		predicates := []Predicate{
			HasAllAnnotationKeys(annotationKeys...),
			HasAnnotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
		if metadata.HasConfigSyncMetadata(o) {
			return fmt.Errorf("object %q shouldn't have configsync metadata (labels: %v, annotations: %v)",
				o.GetName(), o.GetLabels(), o.GetAnnotations())
		}
		return nil
	}
}

// AllResourcesAreCurrent ensures that the managed resources
// are all Current in the ResourceGroup CR.
func AllResourcesAreCurrent() Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
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

// DeploymentHasContainer checks that the Deployment exists and contains the
// specified container.
func DeploymentHasContainer(containerName string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(o, d)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				return nil
			}
		}
		return errors.Errorf("Deployment %s does not have container %s", d.Name, containerName)
	}
}

// DeploymentMissingContainer checks that the Deployment exists and does not contain
// the specified container
func DeploymentMissingContainer(containerName string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return WrongTypeErr(o, d)
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				return errors.Errorf("Deployment %s has container %s", d.Name, containerName)
			}
		}
		return nil
	}
}

// ObjectHasAnnotation checks that the Object has the specified annotation key
func ObjectHasAnnotation(key string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		annotations := o.GetAnnotations()
		for k := range annotations {
			if k == key {
				return nil
			}
		}
		return errors.Errorf("%s missing annotation (%s). Got %v", o.GetName(), key, annotations)
	}
}

// DeploymentHasEnvVar check whether the deployment contains environment variable
// with specified name and value
func DeploymentHasEnvVar(containerName, key, value string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
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
		if o == nil {
			return ErrObjectNotFound
		}
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
func IsManagedBy(scheme *runtime.Scheme, scope declared.Scope, syncName string) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		// Make sure GVK is populated (it's usually not for typed structs).
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = kinds.Lookup(obj, scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
		}

		// managed is required by differ.ManagedByConfigSync
		managedValue := core.GetAnnotation(obj, metadata.ResourceManagementKey)
		if managedValue != metadata.ResourceManagementEnabled {
			return errors.Errorf("expected %s %s to be managed by configsync, but found %q=%q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				metadata.ResourceManagementKey, managedValue)
		}

		// manager is required by diff.IsManager
		expectedManager := declared.ResourceManager(scope, syncName)
		managerValue := core.GetAnnotation(obj, metadata.ResourceManagerKey)
		if managerValue != expectedManager {
			return errors.Errorf("expected %s %s to be managed by %q, but found %q=%q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				expectedManager, metadata.ResourceManagerKey, managerValue)
		}

		// resource-id is required by differ.ManagedByConfigSync
		expectedID := core.GKNN(obj)
		resourceIDValue := core.GetAnnotation(obj, metadata.ResourceIDKey)
		if resourceIDValue != expectedID {
			return errors.Errorf("expected %s %s to have resource-id %q, but found %q=%q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				expectedID, metadata.ResourceIDKey, resourceIDValue)
		}
		return nil
	}
}

// IsNotManaged checks that the object is NOT managed by configsync.
// Use differ.ManagedByConfigSync if you just need a boolean without errors.
func IsNotManaged(scheme *runtime.Scheme) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		// Make sure GVK is populated (it's usually not for typed structs).
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = kinds.Lookup(obj, scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
		}

		// manager is required by diff.IsManager.
		managerValue := core.GetAnnotation(obj, metadata.ResourceManagerKey)
		if managerValue != "" {
			return errors.Errorf("expected %s %s to NOT have a manager, but found %q=%q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				metadata.ResourceManagerKey, managerValue)
		}

		// managed is required by differ.ManagedByConfigSync
		managedValue := core.GetAnnotation(obj, metadata.ResourceManagementKey)
		if managedValue == metadata.ResourceManagementEnabled {
			return errors.Errorf("expected %s %s to NOT have management enabled, but found %q=%q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				metadata.ResourceManagementKey, managedValue)
		}
		return nil
	}
}

// ResourceVersionEquals checks that the object's ResourceVersion matches the
// specified value.
func ResourceVersionEquals(scheme *runtime.Scheme, expected string) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		resourceVersion := obj.GetResourceVersion()
		if resourceVersion == expected {
			return nil
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = kinds.Lookup(obj, scheme)
			if err != nil {
				return err
			}
		}
		return errors.Errorf("expected %s %s to have resourceVersion %q, but got %q",
			gvk.Kind, core.ObjectNamespacedName(obj),
			expected, resourceVersion)
	}
}

// ResourceVersionNotEquals checks that the object's ResourceVersion does NOT
// match specified value.
func ResourceVersionNotEquals(scheme *runtime.Scheme, unexpected string) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		resourceVersion := obj.GetResourceVersion()
		if resourceVersion != unexpected {
			return nil
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Empty() {
			var err error
			gvk, err = kinds.Lookup(obj, scheme)
			if err != nil {
				return err
			}
		}
		return errors.Errorf("expected %s %s to NOT have resourceVersion %q, but got %q",
			gvk.Kind, core.ObjectNamespacedName(obj),
			unexpected, resourceVersion)
	}
}

// HasGenerationAtLeast checks that the object's Generation is greater than or
// equal to the specified value.
func HasGenerationAtLeast(minGeneration int64) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		gen := obj.GetGeneration()
		if gen < minGeneration {
			return fmt.Errorf("expected generation of at least %d, but found %d", minGeneration, gen)
		}
		return nil
	}
}

// StatusEquals checks that the object's computed status matches the specified
// status.
func StatusEquals(scheme *runtime.Scheme, expected status.Status) Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return ErrObjectNotFound
		}
		uObj, err := kinds.ToUnstructured(obj, scheme)
		if err != nil {
			return errors.Wrapf(err, "failed to convert %T %s to unstructured",
				obj, core.ObjectNamespacedName(obj))
		}
		gvk := obj.GetObjectKind().GroupVersionKind()

		result, err := status.Compute(uObj)
		if err != nil {
			return errors.Wrapf(err, "failed to compute status for %s %s",
				gvk.Kind, core.ObjectNamespacedName(obj))
		}

		if result.Status != expected {
			return errors.Errorf("expected %s %s to have status %q, but got %q",
				gvk.Kind, core.ObjectNamespacedName(obj),
				expected, result.Status)
		}
		return nil
	}
}

// SecretHasKey checks that the secret contains key with value
func SecretHasKey(key, value string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		secret := o.(*corev1.Secret)
		actual, ok := secret.Data[key]
		if !ok {
			return errors.Errorf("expected key %s not found in secret %s/%s", key, secret.GetNamespace(), secret.GetName())
		}
		if string(actual) != value {
			return errors.Errorf("expected secret %s/%s to have %s=%s, but got %s=%s",
				secret.GetNamespace(), secret.GetName(), key, value, key, actual)
		}
		return nil
	}
}

// SecretMissingKey checks that the secret does not contain key
func SecretMissingKey(key string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		secret := o.(*corev1.Secret)
		_, ok := secret.Data[key]
		if ok {
			return errors.Errorf("expected key %s to be missing from secret %s/%s, but was found", key, secret.GetNamespace(), secret.GetName())
		}
		return nil
	}
}

// RoleBindingHasName will check the Rolebindings name and compare it with expected value
func RoleBindingHasName(expectedName string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		actualName := o.(*rbacv1.RoleBinding).RoleRef.Name
		if actualName != expectedName {
			return errors.Errorf("Expected name: %s, got: %s", expectedName, actualName)
		}
		return nil
	}
}

// RootSyncHasSourceError returns an error if the RootSync does not have the
// specified Source error code and (optional, partial) message.
func RootSyncHasSourceError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}
		return testutils.ValidateError(rs.Status.Source.Errors, errCode, errMessage)
	}
}

// RepoSyncHasSourceError returns an error if the RootSync does not have the
// specified Source error code and (optional, partial) message.
func RepoSyncHasSourceError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}
		return testutils.ValidateError(rs.Status.Source.Errors, errCode, errMessage)
	}
}

// RootSyncHasRenderingError returns an error if the RootSync does not have the
// specified Rendering error code and (optional, partial) message.
func RootSyncHasRenderingError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}
		return testutils.ValidateError(rs.Status.Rendering.Errors, errCode, errMessage)
	}
}

// RootSyncHasObservedGenerationNoLessThan returns an error if the RootSync has the observedGeneration
// less than the expected generation.
func RootSyncHasObservedGenerationNoLessThan(generation int64) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}
		if rs.Status.ObservedGeneration < generation {
			return errors.Errorf("expected %s %s to have observedGeneration no less than %d, but got %d",
				rs.Kind, core.ObjectNamespacedName(rs),
				generation, rs.Status.ObservedGeneration)
		}
		return nil
	}
}

// RepoSyncHasRenderingError returns an error if the RootSync does not have the
// specified Rendering error code and (optional, partial) message.
func RepoSyncHasRenderingError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}
		return testutils.ValidateError(rs.Status.Rendering.Errors, errCode, errMessage)
	}
}

// RootSyncHasSyncError returns an error if the RootSync does not have the
// specified Sync error code and (optional, partial) message.
func RootSyncHasSyncError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}
		return testutils.ValidateError(rs.Status.Sync.Errors, errCode, errMessage)
	}
}

// RepoSyncHasSyncError returns an error if the RootSync does not have the
// specified Sync error code and (optional, partial) message.
func RepoSyncHasSyncError(errCode, errMessage string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}
		return testutils.ValidateError(rs.Status.Sync.Errors, errCode, errMessage)
	}
}

// HasFinalizer returns a predicate that tests that an Object has the specified finalizer.
func HasFinalizer(name string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		for _, finalizer := range o.GetFinalizers() {
			if finalizer == name {
				return nil
			}
		}
		return fmt.Errorf("expected finalizer %q not found", name)
	}
}

// MissingFinalizer returns a predicate that tests that an Object does NOT have the specified finalizer.
func MissingFinalizer(name string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		for _, finalizer := range o.GetFinalizers() {
			if finalizer == name {
				return fmt.Errorf("expected finalizer %q to be missing", name)
			}
		}
		return nil
	}
}

// HasDeletionTimestamp is a predicate that tests that an Object has a DeletionTimestamp.
func HasDeletionTimestamp(o client.Object) error {
	if o == nil {
		return ErrObjectNotFound
	}
	if o.GetDeletionTimestamp().IsZero() {
		return errors.New("expected deletion timestamp not found")
	}
	return nil
}

// MissingDeletionTimestamp is a predicate that tests that an Object does NOT have a DeletionTimestamp.
func MissingDeletionTimestamp(o client.Object) error {
	if o == nil {
		return ErrObjectNotFound
	}
	if !o.GetDeletionTimestamp().IsZero() {
		return errors.New("expected deletion timestamp to be missing")
	}
	return nil
}

// ObjectNotFoundPredicate returns an error unless the object is nil (not found).
func ObjectNotFoundPredicate(scheme *runtime.Scheme) Predicate {
	return func(o client.Object) error {
		if o == nil {
			// Success! Object Deleted.
			return nil
		}
		// If you see this error, the WatchObject timeout was probably reached.
		return errors.Errorf("expected %T object %s to be not found:\n%s",
			o, core.ObjectNamespacedName(o), log.AsYAMLWithScheme(o, scheme))
	}
}

// ObjectFoundPredicate returns ErrObjectNotFound if the object is nil (not found).
func ObjectFoundPredicate(o client.Object) error {
	if o == nil {
		return ErrObjectNotFound
	}
	return nil
}

// WatchSyncPredicate returns a predicate and a channel.
// The channel will be closed when the predicate is first called.
// Use this to block until WatchObject has completed its first LIST call.
// This will help avoid missed events when WatchObject is run asynchronously.
func WatchSyncPredicate() (Predicate, <-chan struct{}) {
	var once sync.Once
	syncCh := make(chan struct{})
	return func(_ client.Object) error {
		once.Do(func() {
			close(syncCh)
		})
		return nil
	}, syncCh
}
