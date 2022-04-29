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

package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
)

// AllVersionNames returns the set of names of all resources with the specified GroupKind.
func AllVersionNames(resources map[schema.GroupVersionKind][]*unstructured.Unstructured, gk schema.GroupKind) map[string]bool {
	names := map[string]bool{}
	for gvk, rs := range resources {
		if gvk.GroupKind() != gk {
			continue
		}
		for _, r := range rs {
			n := r.GetName()
			if names[n] {
				panic(fmt.Errorf("duplicate resources names %q declared for %s", n, gvk))
			} else {
				names[n] = true
			}
		}
	}
	return names
}

// cmeForNamespace returns a ConfigManagementError for the given Namespace and error message.
func cmeForNamespace(ns *corev1.Namespace, errMsg string) v1.ConfigManagementError {
	e := v1.ErrorResource{
		SourcePath:        ns.GetAnnotations()[metadata.SourcePathAnnotationKey],
		ResourceName:      ns.GetName(),
		ResourceNamespace: ns.GetNamespace(),
		ResourceGVK:       ns.GroupVersionKind(),
	}
	cme := v1.ConfigManagementError{
		ErrorMessage: errMsg,
	}
	cme.ErrorResources = append(cme.ErrorResources, e)
	return cme
}

// clusterConfigNeedsUpdate returns true if the given ClusterConfig will need a status update with the other given arguments.
// initTime is the syncer-controller's instantiation time. It skips updating the sync state if the
// import time is stale (not after the init time) so the repostatus-reconciler can force-update everything on startup.
// An update is needed
// - if the sync state is not synced, or
// - if the sync time is stale (not after the init time), or
// - if errors occur, or
// - if resourceConditions are not empty.
func clusterConfigNeedsUpdate(config *v1.ClusterConfig, initTime metav1.Time, errs []v1.ConfigManagementError, resConditions []v1.ResourceCondition) bool {
	if !config.Spec.ImportTime.After(initTime.Time) {
		klog.V(3).Infof("Ignoring previously imported config %q", config.Name)
		return false
	}
	return !config.Status.SyncState.IsSynced() ||
		config.Status.Token != config.Spec.Token ||
		!config.Status.SyncTime.After(initTime.Time) ||
		len(errs) > 0 ||
		len(config.Status.SyncErrors) > 0 ||
		len(resConditions) > 0 ||
		len(config.Status.ResourceConditions) > 0
}

// SetClusterConfigStatus updates the status sub-resource of the ClusterConfig based on reconciling the ClusterConfig.
// initTime is the syncer-controller's instantiation time. It is used to avoid updating
// the sync state and the sync time for the imported stale configs, so that the
// repostatus-reconciler can force-update the stalled configs on startup.
func SetClusterConfigStatus(ctx context.Context, c *syncerclient.Client, config *v1.ClusterConfig, initTime metav1.Time, now func() metav1.Time, errs []v1.ConfigManagementError, rcs []v1.ResourceCondition) status.Error {
	if !clusterConfigNeedsUpdate(config, initTime, errs, rcs) {
		klog.V(3).Infof("Status for ClusterConfig %q is already up-to-date.", config.Name)
		return nil
	}

	updateFn := func(obj client.Object) (client.Object, error) {
		newConfig := obj.(*v1.ClusterConfig)
		newConfig.Status.Token = config.Spec.Token
		newConfig.Status.SyncTime = now()
		newConfig.Status.ResourceConditions = rcs
		newConfig.Status.SyncErrors = errs
		if len(errs) > 0 {
			newConfig.Status.SyncState = v1.StateError
		} else {
			newConfig.Status.SyncState = v1.StateSynced
		}

		return newConfig, nil
	}
	_, err := c.UpdateStatus(ctx, config, updateFn)
	return err
}

// annotationsHaveResourceCondition checks if the given annotations contain at least one resource condition
func annotationsHaveResourceCondition(annotations map[string]string) bool {
	if _, ok := annotations[metadata.ResourceStatusErrorsKey]; ok {
		return true
	}
	if _, ok := annotations[metadata.ResourceStatusReconcilingKey]; ok {
		return true
	}
	return false
}

// makeResourceCondition makes a resource condition from an unstructured object and the given config token
func makeResourceCondition(obj unstructured.Unstructured, token string) v1.ResourceCondition {
	resourceCondition := v1.ResourceCondition{ResourceState: v1.ResourceStateHealthy, Token: token}
	resourceCondition.GroupVersion = obj.GroupVersionKind().GroupVersion().String()
	resourceCondition.Kind = obj.GroupVersionKind().Kind
	resourceCondition.NamespacedName = fmt.Sprintf("%v/%v", obj.GetNamespace(), obj.GetName())

	if val, ok := obj.GetAnnotations()[metadata.ResourceStatusReconcilingKey]; ok {
		resourceCondition.ResourceState = v1.ResourceStateReconciling
		var reconciling []string
		err := json.Unmarshal([]byte(val), &reconciling)
		if err != nil {
			klog.Errorf("Invalid resource state reconciling annotation on %v %v %v", resourceCondition.GroupVersion, resourceCondition.Kind, resourceCondition.NamespacedName)
			reconciling = []string{val}
		}

		resourceCondition.ReconcilingReasons = append(resourceCondition.ReconcilingReasons, reconciling...)
	}
	if val, ok := obj.GetAnnotations()[metadata.ResourceStatusErrorsKey]; ok {
		resourceCondition.ResourceState = v1.ResourceStateError
		var errs []string
		err := json.Unmarshal([]byte(val), &errs)
		if err != nil {
			klog.Errorf("Invalid resource state error annotation on %v %v %v", resourceCondition.GroupVersion, resourceCondition.Kind, resourceCondition.NamespacedName)
			errs = []string{val}
		} else {
			resourceCondition.Errors = append(resourceCondition.Errors, errs...)
		}
	}
	return resourceCondition
}

func filterContextCancelled(err error) error {
	if errs, ok := err.(status.MultiError); ok {
		if len(errs.Errors()) == 1 {
			err = errs.Errors()[0]
		} else {
			return filterMultiErrorContextCancelled(errs)
		}
	}
	c := errors.Cause(err)
	if reflect.DeepEqual(c, context.Canceled) {
		return nil
	}
	// http client errors don't implement causer. The underlying error is in one of the struct's fields.
	if ue, ok := c.(*url.Error); ok && reflect.DeepEqual(ue.Err, context.Canceled) {
		return nil
	}
	return err
}

func filterMultiErrorContextCancelled(errs status.MultiError) status.MultiError {
	var filtered status.MultiError
	for _, e := range errs.Errors() {
		if fe := filterContextCancelled(e); fe != nil {
			filtered = status.Append(filtered, fe)
		}
	}
	return filtered
}

// resourcesWithoutSync returns a list of strings representing all group/kind in the
// v1.GenericResources list that are not found in a Sync.
func resourcesWithoutSync(
	resources []v1.GenericResources, toSync []schema.GroupVersionKind) []string {
	declaredGroupKind := map[schema.GroupKind]struct{}{}
	for _, res := range resources {
		declaredGroupKind[schema.GroupKind{Group: res.Group, Kind: res.Kind}] = struct{}{}
	}
	for _, gvk := range toSync {
		delete(declaredGroupKind, gvk.GroupKind())
	}

	var gks []string
	for gk := range declaredGroupKind {
		gks = append(gks, gk.String())
	}
	return gks
}
