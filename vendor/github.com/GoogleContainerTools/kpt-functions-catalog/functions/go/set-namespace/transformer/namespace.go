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
package transformer

import (
	"fmt"
	"strings"

	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn"
)

func SetNamespace(rl *fn.ResourceList) (bool, error) {
	tc := NamespaceTransformer{}
	// Get "namespace" arguments from FunctionConfig
	err := tc.Config(rl.FunctionConfig)
	if err != nil {
		rl.Results = append(rl.Results, fn.ErrorResult(err))
		return false, nil
	}
	// Update "namespace" to the proper resources.
	oldNamespaces := map[string]struct{}{}
	count := tc.Transform(rl.Items, oldNamespaces)
	result := tc.LogResults(rl, count, oldNamespaces)
	rl.Results = append(rl.Results, result)
	return true, nil
}

func (p *NamespaceTransformer) LogResults(rl *fn.ResourceList, count int, oldNamespaces map[string]struct{}) *fn.Result {
	if len(p.Errors) != 0 {
		errMsg := strings.Join(p.Errors, "\n")
		return fn.GeneralResult(errMsg, fn.Error)
	}
	if dupErr := fn.CheckResourceDuplication(rl); dupErr != nil {
		return fn.ErrorResult(dupErr)
	}
	if count == 0 {
		msg := fmt.Sprintf("all namespaces are already %q. no value changed", p.NewNamespace)
		return fn.GeneralResult(msg, fn.Info)
	}

	oldNss := []string{}
	for oldNs := range oldNamespaces {
		oldNss = append(oldNss, `"`+oldNs+`"`)
	}
	msg := fmt.Sprintf("namespace %v updated to %q, %d value(s) changed", strings.Join(oldNss, ","), p.NewNamespace, count)
	return fn.GeneralResult(msg, fn.Info)
}

// Config gets the attributes from different FunctionConfig formats.
func (p *NamespaceTransformer) Config(o *fn.KubeObject) error {
	switch {
	case o.IsEmpty():
		return fmt.Errorf("FunctionConfig is missing. Expect `ConfigMap` or `SetNamespace`")
	case o.IsGVK("v1", "ConfigMap"):
		p.NewNamespace = o.NestedStringOrDie("data", "namespace")
		if p.NewNamespace == "" {
			if o.GetName() == "kptfile.kpt.dev" {
				p.NewNamespace = o.NestedStringOrDie("data", "name")
				if p.NewNamespace == "" {
					return fmt.Errorf("`data.name` should not be empty")
				}
			} else {
				return fmt.Errorf("`data.namespace` should not be empty")
			}
		}
	case o.IsGVK(fnConfigAPIVersion, fnConfigKind):
		p.NewNamespace = o.NestedStringOrDie("namespace")
		if p.NewNamespace == "" {
			return fmt.Errorf("`namespace` should not be empty")
		}
	default:
		return fmt.Errorf("unknown functionConfig Kind=%v ApiVersion=%v, expect `%v` or `ConfigMap`",
			o.GetKind(), o.GetAPIVersion(), fnConfigKind)
	}
	return nil
}

// Transform replace the existing Namespace object, namespace-scoped resources' `metadata.name` and
// other namespace reference fields to the new value.
func (p *NamespaceTransformer) Transform(objects []*fn.KubeObject, oldNamespaces map[string]struct{}) int {
	count := new(int)
	ReplaceNamespace(objects, p.NewNamespace, count, oldNamespaces)
	// Update the resources annotation "config.kubernetes.io/depends-on" which may contain old namespace value.
	dependsOnMap := GetDependsOnMap(objects)
	UpdateAnnotation(objects, p.NewNamespace, oldNamespaces, dependsOnMap)
	return *count
}

// VisitNamespaces iterates the `objects` to execute the `visitor` function on each corresponding namespace field.
func VisitNamespaces(objects []*fn.KubeObject, visitor func(namespace *Namespace)) {
	for _, o := range objects {
		switch {
		// Skip local resource which `kpt live apply` skips.
		case o.IsLocalConfig():
			continue
		case o.IsGVK("v1", "Namespace"):
			namespace := o.NestedStringOrDie("metadata", "name")
			visitor(NewNamespace(o, &namespace))
			o.SetOrDie(&namespace, "metadata", "name")
		case o.IsGVK("apiextensions.k8s.io/v1", "CustomResourceDefinition"):
			namespace := o.NestedStringOrDie("spec", "conversion", "webhook", "clientConfig", "service", "namespace")
			visitor(NewNamespace(o, &namespace))
			o.SetOrDie(&namespace, "spec", "conversion", "webhook", "clientConfig", "service", "namespace")
		case o.IsGVK("apiregistration.k8s.io/v1", "APIService"):
			namespace := o.NestedStringOrDie("spec", "service", "namespace")
			visitor(NewNamespace(o, &namespace))
			o.SetOrDie(&namespace, "spec", "service", "namespace")
		case o.GetKind() == "ClusterRoleBinding" || o.GetKind() == "RoleBinding":
			subjects := o.GetSlice("subjects")
			for _, s := range subjects {
				// Note: Users may not expect to change the subject namespace fields. We shall provide an option
				// to skip changing the namespace in ClusterRolebinding/Rolebinding subjects.
				// See https://github.com/kubernetes-sigs/kustomize/issues/1599
				var ns string
				found, _ := s.Get(&ns, "namespace")
				if found {
					visitor(NewNamespace(o, &ns))
					_ = s.SetNestedField(&ns, "namespace")
				}
			}
			o.SetOrDie(&subjects, "subjects")
		}
		// "IsNamespaceScoped" detects resource scope by checking whether `metadata.namespace` is set.
		// This is valid hypothesis is for custom k8s resources. For k8s resources, the function always gives accurate scope information.
		if o.IsNamespaceScoped() {
			namespace := o.NestedStringOrDie("metadata", "namespace")
			visitor(NewNamespace(o, &namespace))
			o.SetOrDie(&namespace, "metadata", "namespace")
		}
	}
}

// ReplaceNamespace iterates the `objects` to replace the `OldNs` with `newNs` on namespace field.
func ReplaceNamespace(objects []*fn.KubeObject, newNs string, count *int, oldNamespace map[string]struct{}) {
	VisitNamespaces(objects, func(ns *Namespace) {
		if *ns.Ptr != newNs {
			oldNamespace[*ns.Ptr] = struct{}{}
			*ns.Ptr = newNs
			*count += 1
		}
	})
}

// GetDependsOnMap iterates `objects` to get the annotation which contains namespace value.
func GetDependsOnMap(objects []*fn.KubeObject) map[string]bool {
	dependsOnMap := map[string]bool{}
	VisitNamespaces(objects, func(ns *Namespace) {
		key := ns.GetDependsOnAnnotation()
		dependsOnMap[key] = true
	})
	return dependsOnMap
}

// UpdateAnnotation updates the `objects`'s "config.kubernetes.io/depends-on" annotation which contains namespace value.
func UpdateAnnotation(objects []*fn.KubeObject, newNs string, oldNs map[string]struct{}, dependsOnMap map[string]bool) {
	VisitNamespaces(objects, func(ns *Namespace) {
		if ns.DependsOnAnnotation == "" || !namespacedResourcePattern.MatchString(ns.DependsOnAnnotation) {
			return
		}
		segments := strings.Split(ns.DependsOnAnnotation, "/")
		dependsOnkey := dependsOnKeyPattern(segments[groupIdx], segments[kindIdx], segments[nameIdx])
		if ok := dependsOnMap[dependsOnkey]; ok {
			if _, ok := oldNs[segments[namespaceIdx]]; ok {
				segments[namespaceIdx] = newNs
				newAnnotation := strings.Join(segments, "/")
				ns.SetDependsOnAnnotation(newAnnotation)
			}
		}
	})
}

type NamespaceTransformer struct {
	NewNamespace string
	DependsOnMap map[string]bool
	Errors       []string
}

func NewNamespace(obj *fn.KubeObject, namespacePtr *string) *Namespace {
	annotationSetter := func(newAnnotation string) {
		obj.SetAnnotation(dependsOnAnnotation, newAnnotation)
	}
	annotationGetter := func() string {
		group := obj.GetAPIVersion()
		if i := strings.Index(obj.GetAPIVersion(), "/"); i > -1 {
			group = group[:i]
		}
		return dependsOnKeyPattern(group, obj.GetKind(), obj.GetName())
	}
	return &Namespace{
		Ptr:                 namespacePtr, //  obj.GetStringOrDie(path...),
		IsNamespace:         obj.IsGVK("v1", "Namespace"),
		DependsOnAnnotation: obj.GetAnnotations()[dependsOnAnnotation],
		annotationGetter:    annotationGetter,
		annotationSetter:    annotationSetter,
	}
}

type Namespace struct {
	Ptr                 *string
	IsNamespace         bool
	DependsOnAnnotation string
	annotationGetter    func() string
	annotationSetter    func(newDependsOnAnnotation string)
}

func (n *Namespace) SetDependsOnAnnotation(newDependsOn string) {
	n.annotationSetter(newDependsOn)
}

func (n *Namespace) GetDependsOnAnnotation() string {
	return n.annotationGetter()
}
