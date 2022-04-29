// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package ordering

import (
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/cli-utils/pkg/object"
)

type SortableInfos []*resource.Info

var _ sort.Interface = SortableInfos{}

func (a SortableInfos) Len() int      { return len(a) }
func (a SortableInfos) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortableInfos) Less(i, j int) bool {
	first, err := object.InfoToObjMeta(a[i])
	if err != nil {
		return false
	}
	second, err := object.InfoToObjMeta(a[j])
	if err != nil {
		return false
	}
	return less(first, second)
}

type SortableUnstructureds []*unstructured.Unstructured

var _ sort.Interface = SortableUnstructureds{}

func (a SortableUnstructureds) Len() int      { return len(a) }
func (a SortableUnstructureds) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortableUnstructureds) Less(i, j int) bool {
	first := object.UnstructuredToObjMetadata(a[i])
	second := object.UnstructuredToObjMetadata(a[j])
	return less(first, second)
}

type SortableMetas []object.ObjMetadata

var _ sort.Interface = SortableMetas{}

func (a SortableMetas) Len() int      { return len(a) }
func (a SortableMetas) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortableMetas) Less(i, j int) bool {
	return less(a[i], a[j])
}

func less(i, j object.ObjMetadata) bool {
	if !Equals(i.GroupKind, j.GroupKind) {
		return IsLessThan(i.GroupKind, j.GroupKind)
	}
	// In case of tie, compare the namespace and name combination so that the output
	// order is consistent irrespective of input order
	if i.Namespace != j.Namespace {
		return i.Namespace < j.Namespace
	}
	return i.Name < j.Name
}

var groupKind2index = computeGroupKind2index()

func computeGroupKind2index() map[schema.GroupKind]int {
	// An attempt to order things to help k8s, e.g.
	// a Service should come before things that refer to it.
	// Namespace should be first.
	// In some cases order just specified to provide determinism.
	orderFirst := []schema.GroupKind{
		{Group: "", Kind: "Namespace"},
		{Group: "", Kind: "ResourceQuota"},
		{Group: "storage.k8s.io", Kind: "StorageClass"},
		{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"},
		{Group: "admissionregistration.k8s.io", Kind: "MutatingWebhookConfiguration"},
		{Group: "", Kind: "ServiceAccount"},
		{Group: "extensions", Kind: "PodSecurityPolicy"}, // deprecated=1.11, removed=1.16
		{Group: "policy", Kind: "PodSecurityPolicy"},     // deprecated=1.21, removed=1.25
		{Group: "rbac.authorization.k8s.io", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Kind: "RoleBinding"},
		{Group: "rbac.authorization.k8s.io", Kind: "ClusterRoleBinding"},
		{Group: "", Kind: "ConfigMap"},
		{Group: "", Kind: "Secret"},
		{Group: "", Kind: "Service"},
		{Group: "", Kind: "LimitRange"},
		{Group: "scheduling.k8s.io", Kind: "PriorityClass"},
		{Group: "extensions", Kind: "Deployment"}, // deprecated=1.8, removed=1.16
		{Group: "apps", Kind: "Deployment"},
		{Group: "apps", Kind: "StatefulSet"},
		{Group: "batch", Kind: "CronJob"},
		{Group: "policy", Kind: "PodDisruptionBudget"},
	}
	orderLast := []schema.GroupKind{
		{Group: "admissionregistration.k8s.io", Kind: "ValidatingWebhookConfiguration"},
	}
	kind2indexResult := make(map[schema.GroupKind]int, len(orderFirst)+len(orderLast))
	for i, n := range orderFirst {
		kind2indexResult[n] = -len(orderFirst) + i
	}
	for i, n := range orderLast {
		kind2indexResult[n] = 1 + i
	}
	return kind2indexResult
}

func Equals(i, j schema.GroupKind) bool {
	return i.Group == j.Group && i.Kind == j.Kind
}

func IsLessThan(i, j schema.GroupKind) bool {
	indexI := groupKind2index[i]
	indexJ := groupKind2index[j]
	if indexI != indexJ {
		return indexI < indexJ
	}
	if i.Group != j.Group {
		return i.Group < j.Group
	}
	return i.Kind < j.Kind
}
