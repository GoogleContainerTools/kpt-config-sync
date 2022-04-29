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
	"context"
	"fmt"
	"io"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestAssembleLogSources(t *testing.T) {
	tests := []struct {
		name           string
		ns             v1.Namespace
		pods           v1.PodList
		expectedValues logSources
	}{
		{
			name:           "No pods",
			ns:             *fake.NamespaceObject("foo"),
			pods:           v1.PodList{Items: make([]v1.Pod, 0)},
			expectedValues: make(logSources, 0),
		},
		{
			name: "Multiple pods with various container configurations",
			ns:   *fake.NamespaceObject("foo"),
			pods: v1.PodList{Items: []v1.Pod{
				*fake.PodObject("foo_a", []v1.Container{
					*fake.ContainerObject("1"),
					*fake.ContainerObject("2"),
				}),
				*fake.PodObject("foo_b", []v1.Container{
					*fake.ContainerObject("3"),
				}),
			}},
			expectedValues: logSources{
				&logSource{
					ns: *fake.NamespaceObject("foo"),
					pod: *fake.PodObject("foo_a", []v1.Container{
						*fake.ContainerObject("1"),
						*fake.ContainerObject("2"),
					}),
					cont: *fake.ContainerObject("1"),
				},
				&logSource{
					ns: *fake.NamespaceObject("foo"),
					pod: *fake.PodObject("foo_a", []v1.Container{
						*fake.ContainerObject("1"),
						*fake.ContainerObject("2"),
					}),
					cont: *fake.ContainerObject("2"),
				},
				&logSource{
					ns: *fake.NamespaceObject("foo"),
					pod: *fake.PodObject("foo_b", []v1.Container{
						*fake.ContainerObject("3"),
					}),
					cont: *fake.ContainerObject("3"),
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
