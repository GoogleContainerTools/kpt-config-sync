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

package namespaceconfig

import (
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AllConfigs holds things that Importer wants to sync. It is only used in-process, not written
// directly as a Kubernetes resource.
type AllConfigs struct {
	// Map of names to NamespaceConfigs.
	NamespaceConfigs map[string]v1.NamespaceConfig
	// Singleton config for non-CRD cluster-scoped resources.
	ClusterConfig *v1.ClusterConfig
	// Config with declared state for CRDs.
	CRDClusterConfig *v1.ClusterConfig
	// Map of names to Syncs.
	Syncs map[string]v1.Sync
}

// NewAllConfigs initializes a default empty AllConfigs.
func NewAllConfigs(importToken string, loadTime metav1.Time, fileObjects []ast.FileObject) *AllConfigs {
	result := &AllConfigs{
		NamespaceConfigs: map[string]v1.NamespaceConfig{},
		ClusterConfig:    v1.NewClusterConfig(importToken, loadTime),
		CRDClusterConfig: v1.NewCRDClusterConfig(importToken, loadTime),
		Syncs:            map[string]v1.Sync{},
	}

	// Sort cluster-scoped objects first.
	// This ensures that if a Namespace is declared, it is inserted before any
	// objects that would go inside it.
	sort.Slice(fileObjects, func(i, j int) bool {
		iNamespaced := fileObjects[i].GetNamespace() != ""
		jNamespaced := fileObjects[j].GetNamespace() != ""
		if iNamespaced != jNamespaced {
			return jNamespaced
		}
		return false
	})

	for _, f := range fileObjects {
		if transform.IsEphemeral(f.GetObjectKind().GroupVersionKind()) {
			// Do not materialize NamespaceSelectors.
			continue
		}

		if f.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
			// Namespace is a snowflake.
			// This preserves the ordering behavior of kubectl apply -f. This means what is in the
			// alphabetically-last file wins.
			result.addNamespaceConfig(f.GetName(), importToken, loadTime, f.GetAnnotations(), f.GetLabels())
			continue
		}

		result.addSync(*v1.NewSync(f.GetObjectKind().GroupVersionKind().GroupKind()))

		isNamespaced := f.GetNamespace() != ""
		if !isNamespaced {
			// The object is cluster-scoped.
			result.addClusterResource(f.Unstructured)
			continue
		}

		// The object is namespace-scoped.
		namespace := f.GetNamespace()
		result.addNamespaceResource(namespace, importToken, loadTime, f.Unstructured)
	}

	return result
}

// ClusterScopedCount returns the number of cluster-scoped resources in the AllConfigs.
func (c *AllConfigs) ClusterScopedCount() int {
	if c == nil {
		return 0
	}
	count := 0
	if c.ClusterConfig != nil {
		count += len(c.ClusterConfig.Spec.Resources)
	}
	if c.CRDClusterConfig != nil {
		count += len(c.CRDClusterConfig.Spec.Resources)
	}
	return count
}

// addClusterResource adds a cluster-scoped resource to the AllConfigs.
func (c *AllConfigs) addClusterResource(o client.Object) {
	if o.GetObjectKind().GroupVersionKind().GroupKind() == kinds.CustomResourceDefinition() {
		// CRDs end up in their own ClusterConfig.
		c.CRDClusterConfig.AddResource(o)
	} else {
		c.ClusterConfig.AddResource(o)
	}
}

// addNamespaceConfig adds a Namespace node to the AllConfigs.
func (c *AllConfigs) addNamespaceConfig(name string, importToken string, loadTime metav1.Time, annotations map[string]string, labels map[string]string) {
	//TODO: What should we do with duplicate Namespaces?
	var resources []v1.GenericResources
	ns, found := c.NamespaceConfigs[name]
	if found {
		resources = ns.Spec.Resources
	}
	ns = *v1.NewNamespaceConfig(name, annotations, labels, importToken, loadTime)
	ns.Spec.Resources = resources
	c.NamespaceConfigs[name] = ns
}

// addNamespaceResource adds an object to a Namespace node, instantiating a default Namespace if
// none exists.
func (c *AllConfigs) addNamespaceResource(namespace string, importToken string, loadTime metav1.Time, o client.Object) {
	ns, found := c.NamespaceConfigs[namespace]
	if !found {
		// Add an implicit Namespace, and mark it "deletion: prevent", which means
		// it won't be deleted when its members are removed from the SOT.
		c.addNamespaceConfig(namespace, importToken, loadTime, map[string]string{
			common.LifecycleDeleteAnnotation: common.PreventDeletion,
		}, nil)
		ns = c.NamespaceConfigs[namespace]
	}
	ns.AddResource(o)
	c.NamespaceConfigs[namespace] = ns
}

// addSync adds a sync to the AllConfigs, adding the required SyncFinalizer.
func (c *AllConfigs) addSync(sync v1.Sync) {
	sync.SetFinalizers(append(sync.GetFinalizers(), v1.SyncFinalizer))
	c.Syncs[sync.Name] = sync
}
