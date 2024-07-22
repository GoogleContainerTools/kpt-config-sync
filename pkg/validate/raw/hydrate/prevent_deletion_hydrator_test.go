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
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/validate/fileobjects"
	"sigs.k8s.io/cli-utils/pkg/common"
)

func TestPreventDeletion(t *testing.T) {
	objs := &fileobjects.Raw{
		Objects: []ast.FileObject{
			k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml", core.Name("reader")),
			k8sobjects.Namespace("namespaces/default"),
			k8sobjects.Namespace("namespaces/kube-system"),
			k8sobjects.Namespace("namespaces/kube-public"),
			k8sobjects.Namespace("namespaces/kube-node-lease"),
			k8sobjects.Namespace("namespaces/gatekeeper-system"),
			k8sobjects.Namespace("namespaces/bookstore"),
		},
	}
	want := &fileobjects.Raw{
		Objects: []ast.FileObject{
			k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml",
				core.Name("reader")),
			k8sobjects.Namespace("namespaces/default", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			k8sobjects.Namespace("namespaces/kube-system", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			k8sobjects.Namespace("namespaces/kube-public", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			k8sobjects.Namespace("namespaces/kube-node-lease", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			k8sobjects.Namespace("namespaces/gatekeeper-system", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			k8sobjects.Namespace("namespaces/bookstore"),
		},
	}

	if err := PreventDeletion(objs); err != nil {
		t.Errorf("Got PreventDeletion() error %v, want nil", err)
	}
	if diff := cmp.Diff(want, objs, ast.CompareFileObject); diff != "" {
		t.Error(diff)
	}
}
