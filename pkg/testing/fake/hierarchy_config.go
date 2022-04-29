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

package fake

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HierarchyConfigKind adds a single GVK to a HierarchyConfig.
func HierarchyConfigKind(mode v1.HierarchyModeType, gvk schema.GroupVersionKind) core.MetaMutator {
	return HierarchyConfigResource(mode, gvk.GroupVersion(), gvk.Kind)
}

// HierarchyConfigResource adds a HierarchyConfigResource to a HierarchyConfig.
func HierarchyConfigResource(mode v1.HierarchyModeType, gv schema.GroupVersion, kinds ...string) core.MetaMutator {
	return func(o client.Object) {
		hc := o.(*v1.HierarchyConfig)
		hc.Spec.Resources = append(hc.Spec.Resources,
			v1.HierarchyConfigResource{
				Group:         gv.Group,
				Kinds:         kinds,
				HierarchyMode: mode,
			})
	}
}

// HierarchyConfig initializes  HierarchyConfig in a FileObject.
func HierarchyConfig(opts ...core.MetaMutator) ast.FileObject {
	return HierarchyConfigAtPath("system/hc.yaml", opts...)
}

// HierarchyConfigObject initializes a HierarchyConfig.
func HierarchyConfigObject(opts ...core.MetaMutator) *v1.HierarchyConfig {
	result := &v1.HierarchyConfig{TypeMeta: ToTypeMeta(kinds.HierarchyConfig())}
	defaultMutate(result)
	for _, opt := range opts {
		opt(result)
	}
	return result
}

// HierarchyConfigAtPath returns a HierarchyConfig at the specified path.
func HierarchyConfigAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(HierarchyConfigObject(opts...), path)
}
