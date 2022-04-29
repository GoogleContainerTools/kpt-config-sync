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

package status

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/graph"
)

func newEdge(obj1, obj2 string) graph.Edge {
	obj1Meta := object.ObjMetadata{
		Namespace: "fake-ns",
		Name:      obj1,
		GroupKind: schema.GroupKind{
			Kind: "fake-kind",
		},
	}

	obj2Meta := object.ObjMetadata{
		Namespace: "fake-ns",
		Name:      obj2,
		GroupKind: schema.GroupKind{
			Kind: "fake-kind",
		},
	}

	return graph.Edge{
		From: obj1Meta,
		To:   obj2Meta,
	}
}

const cyclicErrorMsg = `cyclic dependency:
- /namespaces/fake-ns/fake-kind/obj-1 -> /namespaces/fake-ns/fake-kind/obj-2
- /namespaces/fake-ns/fake-kind/obj-2 -> /namespaces/fake-ns/fake-kind/obj-1`

func TestFormatCyclicDepErr(t *testing.T) {
	tests := []struct {
		name string
		// cyclicError is the error type from cli-utils which helps us detect any upstream changes
		// to the message format.
		cyclicError *graph.CyclicDependencyError
		message     string
		want        string
	}{
		{
			name: "A->B; B->A error from cli-utils",
			cyclicError: &graph.CyclicDependencyError{
				Edges: []graph.Edge{newEdge("obj-1", "obj-2"), newEdge("obj-2", "obj-1")},
			},
			want: "cyclic dependency: /namespaces/fake-ns/fake-kind/obj-1 -> /namespaces/fake-ns/fake-kind/obj-2; /namespaces/fake-ns/fake-kind/obj-2 -> /namespaces/fake-ns/fake-kind/obj-1",
		},
		{
			name:    "Non-cyclic error message",
			message: "etcdserver: request is too large",
			want:    "etcdserver: request is too large",
		},
		{
			name:    "Non-cyclic multiline error message should remain the same",
			message: "fake error: line 1\nline2",
			want:    "fake error: line 1\nline2",
		},
		{
			name:    "Nested cyclic error should also be formatted",
			message: "top-level error: underlying error: " + cyclicErrorMsg,
			want:    "top-level error: underlying error: cyclic dependency: /namespaces/fake-ns/fake-kind/obj-1 -> /namespaces/fake-ns/fake-kind/obj-2; /namespaces/fake-ns/fake-kind/obj-2 -> /namespaces/fake-ns/fake-kind/obj-1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cyclicError != nil {
				tc.message = tc.cyclicError.Error()
			}
			if got := formatCyclicDepErr(tc.message); got != tc.want {
				t.Errorf("formatCyclicDepErr() = %v, want %v", got, tc.want)
			}
		})
	}
}
