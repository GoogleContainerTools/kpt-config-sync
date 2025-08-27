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

package bugreport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func configManagementObject(multiRepo bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   configmanagement.GroupName,
		Version: "v1",
		Kind:    configmanagement.OperatorKind,
	})
	u.SetName(util.ConfigManagementName)
	_ = unstructured.SetNestedField(u.Object, multiRepo, "spec", "enableMultiRepo")

	return u
}

func captureOutput(t *testing.T, f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()
	assert.NoError(t, w.Close())

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func TestNew(t *testing.T) {
	cmMultiRepo := configManagementObject(true)
	cmMonoRepo := configManagementObject(false)

	testCases := []struct {
		name             string
		client           client.Client
		clientset        *k8sfake.Clientset
		expectedErrCount int
		contextErr       error
		expectedOutput   string
		checkFn          func(t *testing.T, br *BugReporter)
	}{
		{
			name:             "no configmanagement object",
			client:           fake.NewClientBuilder().WithScheme(core.Scheme).WithObjects().Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 0,
			expectedOutput:   "ConfigManagement object not found\n",
			checkFn: func(t *testing.T, br *BugReporter) {
				if br.cm.GetName() != "" {
					t.Errorf("expected empty cm object, got %v", br.cm)
				}
			},
		},
		{
			name:             "multi-repo",
			client:           fake.NewClientBuilder().WithObjects(cmMultiRepo).Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 0,
			expectedOutput:   "",
			checkFn: func(t *testing.T, br *BugReporter) {
				if br.cm.GetName() != util.ConfigManagementName {
					t.Errorf("expected cm object, got empty")
				}
				enabled := br.EnabledServices()
				if !enabled[ConfigSync] {
					t.Error("ConfigSync should be enabled for multi-repo")
				}
				if !enabled[ResourceGroup] {
					t.Error("ResourceGroup should be enabled for multi-repo")
				}
			},
		},
		{
			name:             "mono-repo",
			client:           fake.NewClientBuilder().WithObjects(cmMonoRepo).Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 0,
			expectedOutput: "\x1b[33mNotice: The cluster \"context\" is still running in the legacy mode.\n" +
				"Run `nomos migrate` to enable multi-repo mode. " +
				"It provides you with additional features and gives you the flexibility to sync to a single repository, " +
				"or multiple repositories.\x1b[0m\n",
			checkFn: func(t *testing.T, br *BugReporter) {
				if br.cm.GetName() != util.ConfigManagementName {
					t.Errorf("expected cm object, got empty")
				}
			},
		},
		{
			name: "kubeconfig error",

			client:           fake.NewClientBuilder().Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 1,
			expectedOutput:   "ConfigManagement object not found\n",
			contextErr:       errors.New("context error"),
			checkFn: func(t *testing.T, br *BugReporter) {
				if br.k8sContext != "" {
					t.Errorf("expected empty k8sContext, got %q", br.k8sContext)
				}
				if len(br.ErrorList) != 1 {
					t.Fatalf("expected 1 error, got %d", len(br.ErrorList))
				}
			},
		},
		{
			name: "no kind match error",
			client: fake.NewClientBuilder().WithScheme(core.Scheme).WithInterceptorFuncs(
				interceptor.Funcs{
					Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
						return &meta.NoKindMatchError{
							GroupKind: schema.GroupKind{
								Group: configmanagement.GroupName,
								Kind:  configmanagement.OperatorKind,
							},
							SearchedVersions: []string{"v1"},
						}
					},
				}).Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 0,
			expectedOutput:   "kind <<ConfigManagement>> is not registered with the cluster\n",
		},
		{
			name: "generic get error",
			client: fake.NewClientBuilder().WithScheme(core.Scheme).WithInterceptorFuncs(
				interceptor.Funcs{
					Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
						return errors.New("generic get error")
					},
				}).Build(),
			clientset:        k8sfake.NewSimpleClientset(),
			expectedErrCount: 1,
			expectedOutput:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			restconfig.CurrentContextName = func() (string, error) {
				if tc.contextErr != nil {
					return "", tc.contextErr
				}
				return "context", nil
			}

			var br *BugReporter
			var err error
			output := captureOutput(t, func() {
				br, err = New(context.Background(), tc.client, tc.clientset)
			})

			if err != nil {
				t.Fatalf("New() returned an unexpected error: %v", err)
			}

			if len(br.ErrorList) != tc.expectedErrCount {
				t.Errorf("got %d errors, want %d. Errors: %v", len(br.ErrorList), tc.expectedErrCount, br.ErrorList)
			}

			if diff := cmp.Diff(tc.expectedOutput, output); diff != "" {
				t.Errorf("unexpected output (-want +got):\n%s", diff)
			}

			if tc.checkFn != nil {
				tc.checkFn(t, br)
			}
		})
	}
}

func TestAssembleLogSources(t *testing.T) {
	tests := []struct {
		name           string
		ns             v1.Namespace
		pods           v1.PodList
		expectedValues logSources
	}{
		{
			name:           "No pods",
			ns:             *k8sobjects.NamespaceObject("foo"),
			pods:           v1.PodList{Items: make([]v1.Pod, 0)},
			expectedValues: make(logSources, 0),
		},
		{
			name: "Multiple pods with various container configurations",
			ns:   *k8sobjects.NamespaceObject("foo"),
			pods: v1.PodList{Items: []v1.Pod{
				*k8sobjects.PodObject("foo_a", []v1.Container{
					*k8sobjects.ContainerObject("1"),
					*k8sobjects.ContainerObject("2"),
				}),
				*k8sobjects.PodObject("foo_b", []v1.Container{
					*k8sobjects.ContainerObject("3"),
				}),
			}},
			expectedValues: logSources{
				&logSource{
					ns: *k8sobjects.NamespaceObject("foo"),
					pod: *k8sobjects.PodObject("foo_a", []v1.Container{
						*k8sobjects.ContainerObject("1"),
						*k8sobjects.ContainerObject("2"),
					}),
					cont: *k8sobjects.ContainerObject("1"),
				},
				&logSource{
					ns: *k8sobjects.NamespaceObject("foo"),
					pod: *k8sobjects.PodObject("foo_a", []v1.Container{
						*k8sobjects.ContainerObject("1"),
						*k8sobjects.ContainerObject("2"),
					}),
					cont: *k8sobjects.ContainerObject("2"),
				},
				&logSource{
					ns: *k8sobjects.NamespaceObject("foo"),
					pod: *k8sobjects.PodObject("foo_b", []v1.Container{
						*k8sobjects.ContainerObject("3"),
					}),
					cont: *k8sobjects.ContainerObject("3"),
				},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			outputs := assembleLogSources(test.ns, test.pods)

			sort.Sort(outputs)
			sort.Sort(test.expectedValues)

			for i, output := range outputs {
				expected := test.expectedValues[i]
				if diff := cmp.Diff(output, expected, cmp.AllowUnexported(logSource{})); diff != "" {
					t.Errorf("%T differ (-got, +want): %s", expected, diff)
				}
			}
		})
	}
}

type mockLogSource struct {
	returnError bool
	name        string
	readCloser  io.ReadCloser
}

var _ convertibleLogSourceIdentifiers = &mockLogSource{}

// fetchRcForLogSource implements convertibleLogSourceIdentifiers.
func (m *mockLogSource) fetchRcForLogSource(_ context.Context, _ coreClient) (io.ReadCloser, error) {
	if m.returnError {
		return nil, fmt.Errorf("failed to get RC")
	}

	return m.readCloser, nil
}

func (m *mockLogSource) pathName() string {
	return m.name
}

type mockReadCloser struct{}

var _ io.ReadCloser = &mockReadCloser{}

// Read implements io.ReadCloser.
func (m *mockReadCloser) Read(_ []byte) (n int, err error) {
	return 0, nil
}

// Close implements io.ReadCloser.
func (m *mockReadCloser) Close() error {
	return nil
}

// Sorting implementation allows for easy comparison during testing
func (ls logSources) Len() int {
	return len(ls)
}

func (ls logSources) Swap(i, j int) {
	ls[i], ls[j] = ls[j], ls[i]
}

func (ls logSources) Less(i, j int) bool {
	return ls[i].pathName() < ls[j].pathName()
}
