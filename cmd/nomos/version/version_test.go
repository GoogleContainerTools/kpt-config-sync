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

package version

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
)

func TestVersion(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		objects        []runtime.Object
		expected       []string
		currentContext string
		contexts       []string
		configs        map[string]*rest.Config
	}{
		{
			name:     "specify zero clusters",
			version:  "v1.2.3",
			contexts: []string{},
			configs:  nil,
			objects: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       configmanagement.OperatorKind,
						"apiVersion": "configmanagement.gke.io/v1",
						"metadata": map[string]interface{}{
							"name":      "config-management",
							"namespace": "",
						},
						"status": map[string]interface{}{
							"configManagementVersion": "v1.2.3-rc.42",
						},
					},
				},
			},
			expected: []string{
				"CURRENT   CLUSTER_CONTEXT_NAME   COMPONENT     VERSION",
				"                                 <nomos CLI>   v1.2.3",
				"",
			},
		},
		{
			name:    "not installed",
			version: "v2.3.4-rc.5",
			expected: []string{
				"CURRENT   CLUSTER_CONTEXT_NAME   COMPONENT           VERSION",
				"                                 <nomos CLI>         v2.3.4-rc.5",
				"*         config                 config-management   NOT INSTALLED",
				"",
			},
			configs:        map[string]*rest.Config{"config": nil},
			currentContext: "config",
		},
		{
			name:    "installed something",
			version: "v3.4.5-rc.6",
			objects: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"kind":       configmanagement.OperatorKind,
						"apiVersion": "configmanagement.gke.io/v1",
						"metadata": map[string]interface{}{
							"name":      "config-management",
							"namespace": "",
						},
						"status": map[string]interface{}{
							"configManagementVersion": "v1.2.3-rc.42",
						},
					},
				},
			},
			expected: []string{
				util.ColorYellow + "Notice: The cluster \"config\" is still running in the legacy mode.",
				"Run `nomos migrate` to enable multi-repo mode. It provides you with additional features and gives you the flexibility to sync to a single repository, or multiple repositories." + util.ColorDefault,
				"CURRENT   CLUSTER_CONTEXT_NAME   COMPONENT           VERSION",
				"                                 <nomos CLI>         v3.4.5-rc.6",
				"*         config                 config-management   v1.2.3-rc.42",
				"",
			},
			configs:        map[string]*rest.Config{"config": nil},
			currentContext: "config",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			clientVersion = func() string {
				return test.version
			}
			util.DynamicClient = func(c *rest.Config) (dynamic.Interface, error) {
				return fake.NewSimpleDynamicClient(runtime.NewScheme(), test.objects...), nil
			}
			restconfig.CurrentContextName = func() (string, error) {
				return test.currentContext, nil
			}
			var b strings.Builder
			ctx := context.Background()
			versionInternal(ctx, test.configs, &b, test.contexts)
			actuals := strings.Split(b.String(), "\n")
			if diff := cmp.Diff(test.expected, actuals); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
