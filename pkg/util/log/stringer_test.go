// Copyright 2023 Google LLC
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

package log

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// binaryScaleFactor is the factor between binary measurement units, used by
// formatBytes
const binaryScaleFactor float64 = 1024

// binaryByteUnits are the binary measurement units to use in formatBytes.
var binaryByteUnits = [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "XiB"}

// maxMemUseBytes is the max amount of memory AsYAMLDiffWithScheme is allowed to
// use to format the test inputs of TestAsYAMLDiffWithScheme.
// This should help catch regressions that could cause OOMKills in tests.
const maxMemUseBytes uint64 = 1024 * 1024 * 1024 * 2 // 2 GiB

// defaultPrecision is the precision to use when printing byte sizes in
// TestAsYAMLDiffWithScheme.
const defaultPrecision uint = 1

func TestAsYAML(t *testing.T) {
	testCases := []struct {
		name           string
		input          interface{}
		expectedOutput string
	}{
		{
			name: "typed Namespace",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			expectedOutput: `apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  name: example
spec: {}
status: {}
`,
		},
		{
			name: "unstructured namespace",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: `apiVersion: v1
kind: Namespace
metadata:
  name: example
`,
		},
		{
			name: "unstructured object missing kind",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: `apiVersion: v1
metadata:
  name: example
`,
		},
		{
			name: "core.ID (non-object compound struct)",
			input: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: `Group: ""
Kind: Namespace
Name: example
Namespace: ""
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := AsYAML(tc.input).String()
			testutil.AssertEqual(t, tc.expectedOutput, out)
		})
	}
}

func TestAsYAMLDiff(t *testing.T) {
	testCases := []struct {
		name           string
		old, new       interface{}
		expectedOutput string
	}{
		{
			name: "typed Namespace no diff",
			old: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			new: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			expectedOutput: ``, // empty-string means no diff
		},
		{
			name: "typed Namespace with diff",
			old: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Labels: map[string]string{
						"key1": "value1",
					},
				},
			},
			new: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Labels: map[string]string{
						"key2": "value2",
					},
				},
			},
			expectedOutput: ` apiVersion: v1
 kind: Namespace
 metadata:
   creationTimestamp: null
   labels:
-    key1: value1
+    key2: value2
   name: example
 spec: {}
 status: {}
 `,
		},
		{
			name: "unstructured namespace with no diff",
			old: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			new: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: ``, // empty-string means no diff
		},
		{
			name: "unstructured namespace with diff",
			old: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
						"labels": map[string]string{
							"key1": "value1",
						},
					},
				},
			},
			new: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
						"labels": map[string]string{
							"key2": "value2",
						},
					},
				},
			},
			expectedOutput: ` apiVersion: v1
 kind: Namespace
 metadata:
   labels:
-    key1: value1
+    key2: value2
   name: example
 `,
		},
		{
			name: "core.ID (non-object compound struct) with no diff",
			old: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			new: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: ``,
		},
		{
			name: "core.ID (non-object compound struct) with diff",
			old: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			new: core.ID{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: `-Group: ""
-Kind: Namespace
+Group: apps
+Kind: Deployment
 Name: example
 Namespace: ""
 `,
		},
		{
			name: "nil vs typed",
			old:  nil,
			new: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			expectedOutput: `-null
+apiVersion: v1
+kind: Namespace
+metadata:
+  creationTimestamp: null
+  name: example
+spec: {}
+status: {}
 `,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := AsYAMLDiff(tc.old, tc.new).String()
			testutil.AssertEqual(t, tc.expectedOutput, out)
		})
	}
}

func TestAsYAMLDiffWithScheme(t *testing.T) {
	type fields struct {
		Old    client.Object
		New    client.Object
		Scheme *apiruntime.Scheme
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "small unstructured object",
			fields: fields{
				Old: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "stable.example.com/v1",
						"kind":       "CronTab",
						"metadata": map[string]interface{}{
							"name": "example-1",
						},
					},
				},
				New: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name": "example-1",
						},
					},
				},
				Scheme: core.Scheme,
			},
			want: `-apiVersion: stable.example.com/v1
-kind: CronTab
+apiVersion: apps/v1
+kind: Deployment
 metadata:
   name: example-1
 `,
		},
		{
			name: "small cluster role object",
			fields: fields{
				Old:    parseObjectFromFile(t, "../../../e2e/testdata/clusterrole-create.yaml"),
				New:    parseObjectFromFile(t, "../../../e2e/testdata/clusterrole-modify.yaml"),
				Scheme: core.Scheme,
			},
			want: ` apiVersion: rbac.authorization.k8s.io/v1
 kind: ClusterRole
 metadata:
+  labels:
+    foo: bar
   name: e2e-test-clusterrole
 rules:
 - apiGroups:
   - ""
   resources:
   - deployments
   verbs:
   - get
   - list
+  - create
 `,
		},
		{
			name: "resource group with 1k objects and 1 diff",
			fields: fields{
				Old: func() client.Object {
					obj := newFakeResourceGroup(1000)
					return obj
				}(),
				New: func() client.Object {
					obj := newFakeResourceGroup(1000)
					obj.Spec.Resources[50].Name = "name-50-diff"
					obj.Status.ResourceStatuses[50].Name = "name-50-diff"
					return obj
				}(),
				Scheme: core.Scheme,
			},
			want: func() string {
				var buff strings.Builder
				buff.WriteString(" apiVersion: kpt.dev/v1alpha1\n")
				buff.WriteString(" kind: ResourceGroup\n")
				buff.WriteString(" metadata:\n")
				buff.WriteString("   creationTimestamp: null\n")
				buff.WriteString("   name: root-sync\n")
				buff.WriteString("   namespace: config-management-system\n")
				buff.WriteString(" spec:\n")
				buff.WriteString("   descriptor: {}\n")
				buff.WriteString("   resources:\n")
				for i := range 1000 {
					buff.WriteString("   - group: example/v1\n")
					buff.WriteString("     kind: Example\n")
					if i == 50 {
						buff.WriteString("-    name: name-50\n")
						buff.WriteString("+    name: name-50-diff\n")
					} else {
						buff.WriteString(fmt.Sprintf("     name: name-%d\n", i))
					}
					buff.WriteString("     namespace: example\n")
				}
				buff.WriteString(" status:\n")
				buff.WriteString("   observedGeneration: 0\n")
				buff.WriteString("   resourceStatuses:\n")
				for i := range 1000 {
					buff.WriteString("   - actuation: Succeeded\n")
					buff.WriteString("     group: example/v1\n")
					buff.WriteString("     kind: Example\n")
					if i == 50 {
						buff.WriteString("-    name: name-50\n")
						buff.WriteString("+    name: name-50-diff\n")
					} else {
						buff.WriteString(fmt.Sprintf("     name: name-%d\n", i))
					}
					buff.WriteString("     namespace: example\n")
					buff.WriteString("     reconcile: Succeeded\n")
					buff.WriteString("     status: Current\n")
					buff.WriteString("     strategy: Apply\n")
				}
				// last diff.Diff line always has an empty space
				buff.WriteString(" ")
				return buff.String()
			}(),
		},
		{
			name: "resource group with 1k objects and 1k diffs",
			fields: fields{
				Old: func() client.Object {
					obj := newFakeResourceGroup(1000)
					return obj
				}(),
				New: func() client.Object {
					obj := newFakeResourceGroup(1000)
					for i := range obj.Status.ResourceStatuses {
						obj.Status.ResourceStatuses[i].Reconcile = v1alpha1.ReconcileFailed
						obj.Status.ResourceStatuses[i].Status = v1alpha1.Failed
					}
					return obj
				}(),
				Scheme: core.Scheme,
			},
			want: func() string {
				var buff strings.Builder
				buff.WriteString(" apiVersion: kpt.dev/v1alpha1\n")
				buff.WriteString(" kind: ResourceGroup\n")
				buff.WriteString(" metadata:\n")
				buff.WriteString("   creationTimestamp: null\n")
				buff.WriteString("   name: root-sync\n")
				buff.WriteString("   namespace: config-management-system\n")
				buff.WriteString(" spec:\n")
				buff.WriteString("   descriptor: {}\n")
				buff.WriteString("   resources:\n")
				for i := range 1000 {
					buff.WriteString("   - group: example/v1\n")
					buff.WriteString("     kind: Example\n")
					buff.WriteString(fmt.Sprintf("     name: name-%d\n", i))
					buff.WriteString("     namespace: example\n")
				}
				buff.WriteString(" status:\n")
				buff.WriteString("   observedGeneration: 0\n")
				buff.WriteString("   resourceStatuses:\n")
				for i := range 1000 {
					buff.WriteString("   - actuation: Succeeded\n")
					buff.WriteString("     group: example/v1\n")
					buff.WriteString("     kind: Example\n")
					buff.WriteString(fmt.Sprintf("     name: name-%d\n", i))
					buff.WriteString("     namespace: example\n")
					buff.WriteString("-    reconcile: Succeeded\n")
					buff.WriteString("-    status: Current\n")
					buff.WriteString("+    reconcile: Failed\n")
					buff.WriteString("+    status: Failed\n")
					buff.WriteString("     strategy: Apply\n")
				}
				// last diff.Diff line always has an empty space
				buff.WriteString(" ")
				return buff.String()
			}(),
		},
		{
			name: "resource group with 10k objects and no diffs",
			fields: fields{
				Old:    newFakeResourceGroup(10000),
				New:    newFakeResourceGroup(10000),
				Scheme: core.Scheme,
			},
			want: "yamlDiffStringer: diff disabled: object too large",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldByteSize, err := computeObjectYAMLBytes(tt.fields.Old)
			require.NoError(t, err)
			t.Logf("Old Bytes: %v", formatBytes(oldByteSize, defaultPrecision))
			newByteSize, err := computeObjectYAMLBytes(tt.fields.New)
			require.NoError(t, err)
			t.Logf("New Bytes: %v", formatBytes(newByteSize, defaultPrecision))

			var m1, m2 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m1)
			got := AsYAMLDiffWithScheme(tt.fields.Old, tt.fields.New, tt.fields.Scheme).String()
			runtime.ReadMemStats(&m2)
			yamlSizeDiff := uint64(binary.Size([]byte(got)))
			t.Logf("Diff Bytes: %v", formatBytes(yamlSizeDiff, defaultPrecision))
			memUse := m2.TotalAlloc - m1.TotalAlloc
			t.Logf("Memory Allocated: %v", formatBytes(memUse, defaultPrecision))
			t.Logf("Memory Allocations: %v", m2.Mallocs-m1.Mallocs)
			testutil.AssertEqual(t, tt.want, got)
			require.Less(t, memUse, maxMemUseBytes)
		})
	}
}

func parseObjectFromFile(t *testing.T, filePath string) client.Object {
	fileBytes, err := os.ReadFile(filePath)
	require.NoError(t, err)
	obj := &unstructured.Unstructured{}
	err = yaml.Unmarshal(fileBytes, obj)
	require.NoError(t, err)
	return obj
}

func newFakeResourceGroup(count int) *v1alpha1.ResourceGroup {
	obj := &v1alpha1.ResourceGroup{}
	obj.SetName("root-sync")
	obj.SetNamespace("config-management-system")
	obj.Spec.Resources = make([]v1alpha1.ObjMetadata, count)
	obj.Status.ResourceStatuses = make([]v1alpha1.ResourceStatus, count)
	for i := range count {
		obj.Spec.Resources[i] = v1alpha1.ObjMetadata{
			Name:      fmt.Sprintf("name-%d", i),
			Namespace: "example",
			GroupKind: v1alpha1.GroupKind{
				Group: "example/v1",
				Kind:  "Example",
			},
		}
		obj.Status.ResourceStatuses[i] = v1alpha1.ResourceStatus{
			ObjMetadata: v1alpha1.ObjMetadata{
				Name:      fmt.Sprintf("name-%d", i),
				Namespace: "example",
				GroupKind: v1alpha1.GroupKind{
					Group: "example/v1",
					Kind:  "Example",
				},
			},
			Status:    v1alpha1.Current,
			Strategy:  v1alpha1.Apply,
			Actuation: v1alpha1.ActuationSucceeded,
			Reconcile: v1alpha1.ReconcileSucceeded,
		}
	}
	return obj
}

func computeObjectYAMLBytes(obj client.Object) (uint64, error) {
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return 0, err
	}
	return uint64(binary.Size(yamlBytes)), nil
}

func formatBytes(bytes uint64, precision uint) string {
	// find largest non-zero unit and scale the input
	var f = float64(bytes)
	var i int
	for i = range binaryByteUnits {
		if f < binaryScaleFactor {
			break
		}
		f = f / binaryScaleFactor
	}
	// round the scaled input
	f = roundFloat64(f, precision)
	// zero precision when rounded value has no remainder
	if f == binaryScaleFactor {
		return strconv.FormatFloat(f/binaryScaleFactor, 'f', 0, 64) + binaryByteUnits[i+1]
	}
	// smallest precision possible after rounding
	return strconv.FormatFloat(f, 'f', -1, 64) + binaryByteUnits[i]
}

func roundFloat64(value float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(value*ratio) / ratio
}
