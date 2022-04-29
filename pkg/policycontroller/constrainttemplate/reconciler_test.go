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

package constrainttemplate

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
)

func TestAnnotateConstraintTemplate(t *testing.T) {
	testCases := []struct {
		desc               string
		constraintTemplate unstructured.Unstructured
		want               map[string]string
	}{
		{
			"ConstraintTemplate not yet created",
			ct().generation(5).created(false).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["ConstraintTemplate has not been created"]`,
			},
		},
		{
			"ConstraintTemplate not yet processed",
			ct().generation(5).created(true).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["ConstraintTemplate has not been processed by PolicyController"]`,
			},
		},
		{
			"PolicyController has outdated version of ConstraintTemplate",
			ct().generation(5).created(true).byPod(4).build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["[0] PolicyController has an outdated version of ConstraintTemplate"]`,
			},
		},
		{
			"ConstraintTemplate has two errors",
			ct().generation(5).created(true).byPod(5, "looks bad", "smells bad too").build(),
			map[string]string{
				metadata.ResourceStatusErrorsKey: `["[0] test-code: looks bad","[0] test-code: smells bad too"]`,
			},
		},
		{
			"ConstraintTemplate has error, but is out of date",
			ct().generation(5).created(true).byPod(4, "looks bad").build(),
			map[string]string{
				metadata.ResourceStatusReconcilingKey: `["[0] PolicyController has an outdated version of ConstraintTemplate"]`,
			},
		},
		{
			"ConstraintTemplate is ready",
			ct().generation(5).created(true).byPod(5).build(),
			nil,
		},
		{
			"ConstraintTemplate had annotations previously, but is now ready",
			ct().generation(5).created(true).annotateErrors("looks bad").annotateReconciling("not yet").byPod(5).build(),
			map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			annotateConstraintTemplate(tc.constraintTemplate)
			if diff := cmp.Diff(tc.want, tc.constraintTemplate.GetAnnotations()); diff != "" {
				t.Errorf("Incorrect annotations (-want +got):\n%s", diff)
			}
		})
	}
}

type ctBuilder struct {
	unstructured.Unstructured
}

func ct() *ctBuilder {
	return &ctBuilder{
		Unstructured: unstructured.Unstructured{
			Object: map[string]interface{}{},
		},
	}
}

func (c *ctBuilder) build() unstructured.Unstructured {
	return c.Unstructured
}

func (c *ctBuilder) annotateErrors(msg string) *ctBuilder {
	core.SetAnnotation(c, metadata.ResourceStatusErrorsKey, msg)
	return c
}

func (c *ctBuilder) annotateReconciling(msg string) *ctBuilder {
	core.SetAnnotation(c, metadata.ResourceStatusReconcilingKey, msg)
	return c
}

func (c *ctBuilder) created(cr bool) *ctBuilder {
	_ = unstructured.SetNestedField(c.Object, cr, "status", "created")
	return c
}

func (c *ctBuilder) generation(g int64) *ctBuilder {
	c.SetGeneration(g)
	return c
}

func (c *ctBuilder) byPod(generation int64, errMsgs ...string) *ctBuilder {
	bps, saveChanges := newByPodStatus(c.Object)
	_ = unstructured.SetNestedField(bps, generation, "observedGeneration")

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
