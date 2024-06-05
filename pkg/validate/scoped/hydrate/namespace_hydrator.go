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

package hydrate

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceSelectors hydrates the given Scoped objects by performing namespace
// selection to copy objects into namespaces which match their selector. By
// default, the target namespaces are those statically declared in the source of
// truth. If NamespaceSelector uses the dynamic mode, it also copies objects
// into namespaces that are dynamically present on the cluster. It also sets a
// default namespace on any namespace-scoped object that does not already
// have a namespace or namespace selector set.
func NamespaceSelectors(ctx context.Context, c client.Client, fieldManager string) objects.ScopedVisitor {
	return func(objs *objects.Scoped) status.MultiError {
		nsSelectors, errs := buildSelectorMap(ctx, c, fieldManager, objs)
		if errs != nil {
			return errs
		}
		var result []ast.FileObject
		for _, obj := range objs.Namespace {
			_, hasSelector := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
			if hasSelector {
				copies, err := makeNamespaceCopies(obj, nsSelectors)
				if err != nil {
					errs = status.Append(errs, err)
				} else {
					result = append(result, copies...)
				}
			} else {
				if obj.GetNamespace() == "" {
					if objs.Scope == declared.RootScope {
						obj.SetNamespace(metav1.NamespaceDefault)
					} else {
						obj.SetNamespace(string(objs.Scope))
					}
				}
				result = append(result, obj)
			}
		}
		if errs != nil {
			return errs
		}
		objs.Namespace = result
		return nil
	}
}

// buildSelectorMap processes the given cluster-scoped objects to return a map
// of NamespaceSelector names to the namespaces that are selected by each one.
// Note that this modifies the Scoped objects to filter out the
// NamespaceSelectors since they are no longer needed after this point.
func buildSelectorMap(ctx context.Context, c client.Client, fieldManager string, objs *objects.Scoped) (map[string][]string, status.MultiError) {
	var namespaces, others []ast.FileObject
	nsSelectorMap := make(map[string]labels.Selector)
	selectedNamespaceMap := make(map[string][]labels.Selector)
	var errs status.MultiError
	hasDynamicNSSelector := false

	for _, obj := range objs.Cluster {
		switch obj.GetObjectKind().GroupVersionKind() {
		case kinds.Namespace():
			namespaces = append(namespaces, obj)
		case kinds.NamespaceSelector():
			nsSelector, err := convertToNamespaceSelector(obj)
			if err != nil {
				errs = status.Append(errs, err)
				continue
			}
			selector, err := labelSelector(nsSelector)
			if err != nil {
				errs = status.Append(errs, err)
				continue
			}
			nsSelectorMap[nsSelector.Name] = selector

			if nsSelector.Spec.Mode == v1.NSSelectorDynamicMode {
				hasDynamicNSSelector = true
			} else if nsSelector.Spec.Mode != "" && nsSelector.Spec.Mode != v1.NSSelectorStaticMode {
				errs = status.Append(errs, selectors.UnknownNamespaceSelectorModeError(nsSelector))
			}
		default:
			others = append(others, obj)
		}
	}

	if objs.AllowAPICall && objs.DynamicNSSelectorEnabled != hasDynamicNSSelector {
		// The DynamicNSSelectorEnabled state mismatches with the required state, set the
		// annotation so that the reconciler-manager will recreate the reconciler
		// with the correct setting for the Namespace watch.
		if err := setDynamicNSSelectorEnabled(ctx, c, fieldManager, objs.SyncName, hasDynamicNSSelector); err != nil {
			errs = status.Append(errs, err)
			return nil, errs
		}
	}

	nsList := &corev1.NamespaceList{}
	// Fetch on-cluster Namespaces if allowing API calls and hasDynamicNSSelector,
	// so that the NamespaceSelector can select resources matching on-cluster Namespaces.
	if objs.AllowAPICall && hasDynamicNSSelector {
		if err := c.List(ctx, nsList); err != nil {
			errs = status.Append(errs, selectors.ListNamespaceError(err))
			return nil, errs
		}
	}

	selectorMap := make(map[string][]string)
	for nsSelector, selector := range nsSelectorMap {
		var selectedNamespaces []string
		// Select dynamic/on-cluster Namespaces that match the NamespaceSelector's labels.
		for _, ns := range nsList.Items {
			if ns.Status.Phase != corev1.NamespaceTerminating && selector.Matches(labels.Set(ns.GetLabels())) {
				selectedNamespaces = append(selectedNamespaces, ns.GetName())
				// selectedNamespaceMap only stores dynamic Namespaces.
				selectedNamespaceMap[ns.GetName()] = append(selectedNamespaceMap[ns.GetName()], selector)
			}
		}

		// Select statically declared Namespaces that match the NamespaceSelector's labels.
		for _, namespace := range namespaces {
			if _, found := selectedNamespaceMap[namespace.GetName()]; found {
				// Ignore static Namespaces that are already selected as dynamic
				continue
			}
			if selector.Matches(labels.Set(namespace.GetLabels())) {
				selectedNamespaces = append(selectedNamespaces, namespace.GetName())
			}
		}

		selectorMap[nsSelector] = selectedNamespaces
	}

	if errs != nil {
		return nil, errs
	}

	// We are done with NamespaceSelectors so we can filter them out now.
	objs.Cluster = append(namespaces, others...)

	// Update the NamespaceSelector cache in the Namespace Controller.
	if objs.NSControllerState != nil {
		objs.NSControllerState.SetSelectorCache(nsSelectorMap, selectedNamespaceMap)
	}
	return selectorMap, nil
}

// labelSelector returns the labelSelector from the NamespaceSelector object
func labelSelector(nss *v1.NamespaceSelector) (labels.Selector, status.Error) {
	selector, err := metav1.LabelSelectorAsSelector(&nss.Spec.Selector)
	if err != nil {
		return nil, selectors.InvalidSelectorError(nss, err)
	}
	if selector.Empty() {
		return nil, selectors.EmptySelectorError(nss)
	}
	return selector, nil
}

// makeNamespaceCopies uses the given object's namespace selector to make a copy
// of it into each namespace that is selected by it.
func makeNamespaceCopies(obj ast.FileObject, nsSelectors map[string][]string) ([]ast.FileObject, status.Error) {
	selector := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
	selected, exists := nsSelectors[selector]
	if !exists {
		return nil, selectors.ObjectHasUnknownNamespaceSelector(obj, selector)
	}

	var result []ast.FileObject
	for _, ns := range selected {
		objCopy := obj.DeepCopy()
		objCopy.SetNamespace(ns)
		result = append(result, objCopy)
	}
	return result, nil
}

// convertToNamespaceSelector converts the FileObject into the NamespaceSelector type.
func convertToNamespaceSelector(obj ast.FileObject) (*v1.NamespaceSelector, status.Error) {
	s, sErr := obj.Structured()
	if sErr != nil {
		return nil, sErr
	}
	return s.(*v1.NamespaceSelector), nil
}

// setDynamicNSSelectorEnabled sets the requires-ns-controller annotation on the RootSync object.
func setDynamicNSSelectorEnabled(ctx context.Context, c client.Client, fieldManager, syncName string, hasDynamicNSSelector bool) status.Error {
	rs := &v1beta1.RootSync{}
	objKey := rootsync.ObjectKey(syncName)
	if err := c.Get(ctx, objKey, rs); err != nil {
		return status.APIServerError(err, fmt.Sprintf("failed to get RootSync %s for setting the %s annotation",
			objKey, metadata.DynamicNSSelectorEnabledAnnotationKey))
	}
	newVal := strconv.FormatBool(hasDynamicNSSelector)
	curVal := core.GetAnnotation(rs, metadata.DynamicNSSelectorEnabledAnnotationKey)
	if curVal == newVal {
		// avoid unnecessary updates
		return nil
	}
	klog.Infof("The RootSync annotation %s=%s mismatches the reconciler's env %s=%s, so update the annotation to %s",
		metadata.DynamicNSSelectorEnabledAnnotationKey, curVal, reconcilermanager.DynamicNSSelectorEnabled, newVal, newVal)
	existing := rs.DeepCopy()
	core.SetAnnotation(rs, metadata.DynamicNSSelectorEnabledAnnotationKey, newVal)
	if err := c.Patch(ctx, rs, client.MergeFrom(existing), client.FieldOwner(fieldManager)); err != nil {
		return status.APIServerErrorf(err, fmt.Sprintf("failed to patch RootSync %s to set the %s annotation to %s",
			objKey, metadata.DynamicNSSelectorEnabledAnnotationKey, newVal))
	}
	return nil
}
