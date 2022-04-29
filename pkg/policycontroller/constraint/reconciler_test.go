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

package constraint

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
)

func TestAnnotateConstraint(t *testing.T) {
	testCases := []struct {
		desc       string
		constraint unstructured.Unstructured
		want       map[string]string
	}{
		{
			"Constraint not yet processed",
			con().generation(5).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["Constraint has not been processed by PolicyController"]`,
			},
		},
		{
			"Constraint not yet enforced",
			con().generation(5).byPod(5, false).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["[0] PolicyController is not enforcing Constraint"]`,
			},
		},
		{
			"PolicyController has outdated version of Constraint",
			con().generation(5).byPod(4, true).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["[0] PolicyController has an outdated version of Constraint"]`,
			},
		},
		{
			"ConstraintTemplate has two errors",
			con().generation(5).byPod(5, true, "looks bad", "smells bad too").build(),
			map[string]string{
				metadata.ResourceStatusErrorsKey: `["[0] test-code: looks bad","[0] test-code: smells bad too"]`,
			},
		},
		{
			"ConstraintTemplate has error, but is out of date",
			con().generation(5).byPod(4, true, "looks bad").build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["[0] PolicyController has an outdated version of Constraint"]`,
			},
		},
		{
			"Constraint is ready",
			con().generation(5).byPod(5, true).build(),
			nil,
		},
		{
			"Constraint had annotations previously, but is now ready",
			con().generation(5).annotateErrors("looks bad").annotateReconciling("not yet").byPod(5, true).build(),
			map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			annotateConstraint(tc.constraint)
			if diff := cmp.Diff(tc.want, tc.constraint.GetAnnotations()); diff != "" {
				t.Errorf("Incorrect annotations (-want +got):\n%s", diff)
			}
		})
	}
}

type conBuilder struct {
	unstructured.Unstructured
}

func con() *conBuilder {
	con := &conBuilder{
		Unstructured: unstructured.Unstructured{
			Object: map[string]interface{}{},
		},
	}
	con.SetGroupVersionKind(constraintGV.WithKind("TestConstraint"))
	return con
}

func (c *conBuilder) build() unstructured.Unstructured {
	return c.Unstructured
}

func (c *conBuilder) annotateErrors(msg string) *conBuilder {
	core.SetAnnotation(c, metadata.ResourceStatusErrorsKey, msg)
	return c
}

func (c *conBuilder) annotateReconciling(msg string) *conBuilder {
	core.SetAnnotation(c, metadata.ResourceStatusReconcilingKey, msg)
	return c
}

func (c *conBuilder) generation(g int64) *conBuilder {
	c.SetGeneration(g)
	return c
}

func (c *conBuilder) byPod(generation int64, enforced bool, errMsgs ...string) *conBuilder {
	bps, saveChanges := newByPodStatus(c.Object)
	_ = unstructured.SetNestedField(bps, generation, "observedGeneration")
	_ = unstructured.SetNestedField(bps, enforced, "enforced")

	if len(errMsgs) > 0 {
		var statusErrs []interface{}
		for _, msg := range errMsgs {
			statusErrs = append(statusErrs, map[string]interface{}{
				"code":    "test-code",
				"message": msg,
			})
		}
		_ = unstructured.SetNestedSlice(bps, statusErrs, "errors")
	}

	saveChanges()
	return c
}

// newByPodStatus appends a new byPodStatus to the byPod array of the given
// constraintTemplateStatus. It returns the byPodStatus as well as a function
// to call after the byPodStatus has been mutated to save changes. This function
// is necessary since SetNestedSlice() does a deep copy into the Unstructured.
func newByPodStatus(obj map[string]interface{}) (map[string]interface{}, func()) {
	bpArr, ok, _ := unstructured.NestedSlice(obj, "status", "byPod")
	if !ok {
		bpArr = []interface{}{}
	}

	byPodStatus := map[string]interface{}{}
	id := len(bpArr)
	_ = unstructured.SetNestedField(byPodStatus, fmt.Sprintf("%d", id), "id")
	bpArr = append(bpArr, byPodStatus)

	return byPodStatus, func() {
		_ = unstructured.SetNestedSlice(obj, bpArr, "status", "byPod")
	}
}
