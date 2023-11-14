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
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
	"kpt.dev/configsync/pkg/status"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	devOnlyNSS = fake.NamespaceSelectorObject(core.Name("dev-only"),
		func(o client.Object) {
			o.(*v1.NamespaceSelector).Spec.Selector.MatchLabels = map[string]string{
				"environment": "dev",
			}
		})
	devOnlyNSSFileObject = fake.FileObject(devOnlyNSS, "dev-only-nss.yaml")
	emptyNss             = fake.NamespaceSelector(core.Name("empty"))
	invalidNSSObject     = fake.NamespaceSelectorObject(core.Name("invalid"),
		func(o client.Object) {
			o.(*v1.NamespaceSelector).Spec.Selector.MatchLabels = map[string]string{
				"environment": "xin prod",
			}
		})
	invalidNSS        = fake.FileObject(invalidNSSObject, "invalid-nss.yaml")
	devOnlyDynamicNSS = fake.NamespaceSelectorObject(core.Name("dev-only"),
		func(o client.Object) {
			ns := o.(*v1.NamespaceSelector)
			ns.Spec.Selector.MatchLabels = map[string]string{
				"environment": "dev",
			}
			ns.Spec.Mode = v1.NSSelectorDynamicMode
		})
	devOnlyDynamicNSSFileObject = fake.FileObject(devOnlyDynamicNSS, "dev-only-nss.yaml")
)

func TestNamespaceSelectors(t *testing.T) {
	testCases := []struct {
		name                                   string
		objs                                   *objects.Scoped
		onClusterObjects                       []client.Object
		originalDynamicNSSelectorEnabled       bool
		want                                   *objects.Scoped
		wantErrs                               status.MultiError
		wantDynamicNSSelectorEnabledAnnotation bool
	}{
		{
			name: "No objects",
			objs: &objects.Scoped{},
			want: &objects.Scoped{},
		},
		{
			name: "Keep object with no namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					devOnlyNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("prod")),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("prod")),
				},
			},
		},
		{
			name: "Copy object with active namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					devOnlyNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
					fake.Namespace("namespaces/dev2", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
					fake.Namespace("namespaces/dev2", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(
						core.Namespace("dev1"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
					fake.Role(
						core.Namespace("dev2"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
		},
		{
			name: "Remove object with inactive namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					devOnlyNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
			},
		},
		{
			name: "Set default namespace on namespaced object without namespace or NamespaceSelector",
			objs: &objects.Scoped{
				Scope: "hello",
				Cluster: []ast.FileObject{
					fake.ClusterRole(),
				},
				Namespace: []ast.FileObject{
					fake.Role(),
					fake.Role(core.Namespace("world")),
				},
			},
			want: &objects.Scoped{
				Scope: "hello",
				Cluster: []ast.FileObject{
					fake.ClusterRole(),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("hello")),
					fake.Role(core.Namespace("world")),
				},
			},
		},
		{
			name: "Select namespace-scoped resources in both static namespaces and on-cluster namespaces",
			objs: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					devOnlyDynamicNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			onClusterObjects: []client.Object{
				fake.NamespaceObject("dev2", core.Label("environment", "dev")),
			},
			want: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(
						core.Namespace("dev2"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
					fake.Role(
						core.Namespace("dev1"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			wantDynamicNSSelectorEnabledAnnotation: true,
		},
		{
			name: "Select namespace-scoped resources in both static namespaces and on-cluster namespaces, but they should not be double selected",
			objs: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					devOnlyDynamicNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			onClusterObjects: []client.Object{
				fake.NamespaceObject("dev1", core.Label("environment", "dev")),
				fake.NamespaceObject("dev2", core.Label("environment", "dev")),
			},
			want: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(
						core.Namespace("dev1"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
					fake.Role(
						core.Namespace("dev2"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			wantDynamicNSSelectorEnabledAnnotation: true,
		},
		{
			name: "Unselect namespace-scoped resources in on-cluster namespaces",
			objs: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					devOnlyNSSFileObject,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			onClusterObjects: []client.Object{
				fake.NamespaceObject("dev2", core.Label("environment", "dev")),
			},
			originalDynamicNSSelectorEnabled: true,
			want: &objects.Scoped{
				Scope:    declared.RootReconciler,
				SyncName: configsync.RootSyncName,
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(
						core.Namespace("dev1"),
						core.Annotation(metadata.NamespaceSelectorAnnotationKey, devOnlyNSS.Name)),
				},
			},
			wantDynamicNSSelectorEnabledAnnotation: false,
		},
		{
			name: "Error for missing namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "dev-only")),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "dev-only")),
				},
			},
			wantErrs: selectors.ObjectHasUnknownNamespaceSelector(fake.Role(), "dev-only"),
		},
		{
			name: "Error for empty namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					emptyNss,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "empty")),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					emptyNss,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "empty")),
				},
			},
			wantErrs: selectors.EmptySelectorError(emptyNss),
		},
		{
			name: "Error for invalid namespace selector",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					invalidNSS,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "invalid")),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					invalidNSS,
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Annotation(metadata.NamespaceSelectorAnnotationKey, "invalid")),
				},
			},
			wantErrs: selectors.InvalidSelectorError(invalidNSS, errors.New("")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			s := runtime.NewScheme()
			if err := corev1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}
			if err := v1beta1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}

			rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				core.Annotation(metadata.DynamicNSSelectorEnabledAnnotationKey, strconv.FormatBool(tc.originalDynamicNSSelectorEnabled)))
			tc.onClusterObjects = append(tc.onClusterObjects, rs)
			fakeClient := syncerFake.NewClient(t, s, tc.onClusterObjects...)

			tc.objs.AllowAPICall = true
			tc.objs.DynamicNSSelectorEnabled = tc.originalDynamicNSSelectorEnabled
			tc.objs.NSControllerState = &namespacecontroller.State{}
			errs := NamespaceSelectors(context.Background(), fakeClient)(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("Got NamespaceSelectors() error %v, want %v", errs, tc.wantErrs)
			}
			assert.ElementsMatch(t, tc.want.Cluster, tc.objs.Cluster)
			assert.ElementsMatch(t, tc.want.Namespace, tc.objs.Namespace)
			assert.ElementsMatch(t, tc.want.Unknown, tc.objs.Unknown)
			assert.Equal(t, tc.want.Scope, tc.objs.Scope)
			assert.Equal(t, tc.want.SyncName, tc.objs.SyncName)

			updatedDynamicNSSelectorEnabled, err := dynamicNSSelectorEnabled(fakeClient)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.wantDynamicNSSelectorEnabledAnnotation, updatedDynamicNSSelectorEnabled)
		})
	}
}

func dynamicNSSelectorEnabled(fakeClient client.Client) (bool, error) {
	rs := &v1beta1.RootSync{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Namespace: configsync.ControllerNamespace, Name: configsync.RootSyncName}, rs); err != nil {
		return false, err
	}
	return strconv.ParseBool(rs.Annotations[metadata.DynamicNSSelectorEnabledAnnotationKey])
}
