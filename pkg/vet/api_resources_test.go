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

package vet

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/discovery"
)

func TestAddLines(t *testing.T) {
	testCases := []struct {
		name       string
		lines      string
		wantScopes map[schema.GroupKind]discovery.ScopeType
		wantErr    error
	}{
		{
			name: "standard case",
			lines: `NAME                              SHORTNAMES   APIVERSION                       NAMESPACED   KIND
namespaces                        ns           v1                                     false        Namespace
deployments                       deploy       apps/v1                                true         Deployment
clusterroles                                   rbac.authorization.k8s.io/v1           false        ClusterRole
rolebindings                                   rbac.authorization.k8s.io/v1           true         RoleBinding
configmaps                        cm           v1                                     true         ConfigMap
apiservices                                    apiregistration.k8s.io/v1              false        APIService
customresourcedefinitions         crd,crds     apiextensions.k8s.io/v1                false        CustomResourceDefinition
`,
			wantScopes: map[schema.GroupKind]discovery.ScopeType{
				kinds.Namespace().GroupKind():                  discovery.ClusterScope,
				kinds.Deployment().GroupKind():                 discovery.NamespaceScope,
				kinds.ClusterRole().GroupKind():                discovery.ClusterScope,
				kinds.RoleBinding().GroupKind():                discovery.NamespaceScope,
				kinds.ConfigMap().GroupKind():                  discovery.NamespaceScope,
				kinds.APIService().GroupKind():                 discovery.ClusterScope,
				kinds.CustomResourceDefinitionV1().GroupKind(): discovery.ClusterScope,
			},
			wantErr: nil,
		},
		{
			name: "incorrect APIVERSION column name",
			lines: `NAME                              SHORTNAMES   APIGROUP                       NAMESPACED   KIND
configmaps                        cm           v1                                     true         ConfigMap
namespaces                        ns           v1                                     false        Namespace
apiservices                                    apiregistration.k8s.io/v1              false        APIService
deployments                       deploy       apps/v1                                true         Deployment
namespaceselectors                             configmanagement.gke.io/v1             false        NamespaceSelector
rootsyncs                                      configsync.gke.io/v1beta1              true         RootSync
clusterroles                                   rbac.authorization.k8s.io/v1           false        ClusterRole
poddisruptionbudgets              pdb          policy/v1                              true         PodDisruptionBudget
`,
			wantErr: MissingAPIVersion(""),
		},
		{
			name: "invalid scope value",
			lines: `NAME                              SHORTNAMES   APIVERSION                       NAMESPACED   KIND
configmaps                        cm           v1                                     other         ConfigMap
namespaces                        ns           v1                                     false        Namespace
apiservices                                    apiregistration.k8s.io/v1              false        APIService
deployments                       deploy       apps/v1                                true         Deployment
namespaceselectors                             configmanagement.gke.io/v1             false        NamespaceSelector
rootsyncs                                      configsync.gke.io/v1beta1              true         RootSync
clusterroles                                   rbac.authorization.k8s.io/v1           false        ClusterRole
poddisruptionbudgets              pdb          policy/v1                              true         PodDisruptionBudget 
`,
			wantErr: InvalidScopeValue("", "", "other"),
		},
		{
			name: "missing first line",
			lines: `namespaces                        ns                                          false        Namespace
configmaps                        cm           v1                                     true         ConfigMap
namespaces                        ns           v1                                     false        Namespace
apiservices                                    apiregistration.k8s.io/v1              false        APIService
deployments                       deploy       apps/v1                                true         Deployment
namespaceselectors                             configmanagement.gke.io/v1             false        NamespaceSelector
rootsyncs                                      configsync.gke.io/v1beta1              true         RootSync
clusterroles                                   rbac.authorization.k8s.io/v1           false        ClusterRole
poddisruptionbudgets              pdb          policy/v1                              true         PodDisruptionBudget 
`,
			wantErr: MissingAPIVersion(""),
		},
		{
			name:       "empty data",
			lines:      `NAME                              SHORTNAMES   APIVERSION                       NAMESPACED   KIND `,
			wantScopes: nil,
			wantErr:    nil,
		},
		{
			name: "missing scope value",
			lines: `NAME                              SHORTNAMES   APIVERSION                       NAMESPACED   KIND
configmaps                        cm           v1                                     true         ConfigMap
namespaces                        ns           v1                                     false        Namespace
apiservices                                    apiregistration.k8s.io/v1              false        APIService
deployments                       deploy       apps/v1                                true         Deployment
namespaceselectors                             configmanagement.gke.io/v1             NamespaceSelector
rootsyncs                                      configsync.gke.io/v1beta1              true         RootSync
clusterroles                                   rbac.authorization.k8s.io/v1           false        ClusterRole
poddisruptionbudgets              pdb          policy/v1                              true         PodDisruptionBudget
`,
			wantErr: InvalidScopeValue("", "", "NamespaceSelector"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scoper := discovery.Scoper{}

			gotErr := addLines(&scoper, "", tc.lines)
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("got addLines() error = %v, want %v", gotErr, tc.wantErr)
			}

			for gk, want := range tc.wantScopes {
				got, err := scoper.GetGroupKindScope(gk)
				if err != nil {
					// Scoper error behavior is not under test.
					t.Fatalf("got GetGroupKindScope() err = %v, want nil", err)
				}
				if got != want {
					t.Errorf("got GetGroupKindScope() = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestAddLine(t *testing.T) {
	testCases := []struct {
		name      string
		line      string
		wantErr   error
		groupKind schema.GroupKind
		wantScope discovery.ScopeType
	}{
		{
			name:      "cluster-scoped type",
			line:      "rbac.authorization.k8s.io/v1      false        ClusterRole",
			wantErr:   nil,
			groupKind: kinds.ClusterRole().GroupKind(),
			wantScope: discovery.ClusterScope,
		},
		{
			name:      "namespace-scoped type",
			line:      "apps/v1                        true         Deployment",
			wantErr:   nil,
			groupKind: kinds.Deployment().GroupKind(),
			wantScope: discovery.NamespaceScope,
		},
		{
			name:      "core API group",
			line:      "v1                             false        Namespace",
			wantErr:   nil,
			groupKind: kinds.Namespace().GroupKind(),
			wantScope: discovery.ClusterScope,
		},
		{
			name:      "invalid scope",
			line:      "v1                             other        Namespace",
			wantErr:   InvalidScopeValue("", "", "other"),
			groupKind: kinds.Namespace().GroupKind(),
			wantScope: discovery.UnknownScope,
		},
		{
			name:      "missing scope",
			line:      "v1                             ConfigMap",
			wantErr:   InvalidScopeValue("", "", "ConfigMap"),
			groupKind: kinds.Namespace().GroupKind(),
			wantScope: discovery.UnknownScope,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scoper := discovery.Scoper{}

			gotErr := addLine(&scoper, "", tc.line)
			if !errors.Is(gotErr, tc.wantErr) {
				t.Errorf("got addLine() = %v, want %v", gotErr, tc.wantErr)
			}

			gotScope, err := scoper.GetGroupKindScope(tc.groupKind)
			if err != nil && tc.wantErr == nil {
				// Scoper error behavior is not under test.
				t.Fatalf("got GetGroupKindScope() err = %v, want nil", err)
			}

			if gotScope != tc.wantScope {
				t.Errorf("got GetGroupKindScope() = %v, want %v", gotScope, tc.wantScope)
			}
		})
	}
}
