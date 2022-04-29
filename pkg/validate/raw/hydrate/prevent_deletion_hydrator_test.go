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
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/cli-utils/pkg/common"
)

func TestPreventDeletion(t *testing.T) {
	objs := &objects.Raw{
		Objects: []ast.FileObject{
			fake.ClusterRoleAtPath("cluster/clusterrole.yaml", core.Name("reader")),
			fake.Namespace("namespaces/default"),
			fake.Namespace("namespaces/kube-system"),
			fake.Namespace("namespaces/kube-public"),
			fake.Namespace("namespaces/kube-node-lease"),
			fake.Namespace("namespaces/gatekeeper-system"),
			fake.Namespace("namespaces/bookstore"),
		},
	}
	want := &objects.Raw{
		Objects: []ast.FileObject{
			fake.ClusterRoleAtPath("cluster/clusterrole.yaml",
				core.Name("reader")),
			fake.Namespace("namespaces/default", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			fake.Namespace("namespaces/kube-system", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			fake.Namespace("namespaces/kube-public", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			fake.Namespace("namespaces/kube-node-lease", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			fake.Namespace("namespaces/gatekeeper-system", core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			fake.Namespace("namespaces/bookstore"),
		},
	}

	if err := PreventDeletion(objs); err != nil {
		t.Errorf("Got PreventDeletion() error %v, want nil", err)
	}
	if diff := cmp.Diff(want, objs, ast.CompareFileObject); diff != "" {
		t.Error(diff)
	}
}
