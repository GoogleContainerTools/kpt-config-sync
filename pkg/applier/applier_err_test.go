// Copyright 2024 Google LLC
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

package applier

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestErrorForResourceWithResource(t *testing.T) {
	namespace := "test-namespace"
	namespaceObj := k8sobjects.UnstructuredObject(kinds.Namespace(),
		core.Name(namespace))
	namespaceObjID := core.IDOf(namespaceObj)

	cmObj := k8sobjects.UnstructuredObject(kinds.ConfigMap(),
		core.Name("test-configmap"), core.Namespace(namespace))
	cmObjID := core.IDOf(cmObj)

	anvilObj := k8sobjects.UnstructuredObject(kinds.Anvil(),
		core.Namespace(namespace), core.Name("test-anvil"),
		core.Annotation(metadata.SourcePathAnnotationKey, "foo/anvil.yaml"))
	anvilObjID := core.IDOf(anvilObj)

	testcases := []struct {
		name            string
		err             error
		id              core.ID
		object          client.Object
		expectedMessage string
		expectedCSE     v1beta1.ConfigSyncError
	}{
		{
			name:   "namespace-scoped object",
			err:    errors.New("unknown type"),
			id:     cmObjID,
			object: cmObj,
			expectedMessage: "KNV2009: failed to apply ConfigMap, test-namespace/test-configmap: unknown type\n\n" +
				"namespace: test-namespace\n" +
				"metadata.name: test-configmap\n" +
				"group:\n" +
				"version: v1\n" +
				"kind: ConfigMap\n\n" +
				"For more information, see https://g.co/cloud/acm-errors#knv2009",
			expectedCSE: v1beta1.ConfigSyncError{
				Code: ApplierErrorCode,
				ErrorMessage: "KNV2009: failed to apply ConfigMap, test-namespace/test-configmap: unknown type\n\n" +
					"namespace: test-namespace\n" +
					"metadata.name: test-configmap\n" +
					"group:\n" +
					"version: v1\n" +
					"kind: ConfigMap\n\n" +
					"For more information, see https://g.co/cloud/acm-errors#knv2009",
				Resources: []v1beta1.ResourceRef{
					{
						Name:      "test-configmap",
						Namespace: "test-namespace",
						GVK: metav1.GroupVersionKind{
							Version: "v1",
							Kind:    "ConfigMap",
						},
					},
				},
			},
		},
		{
			name:   "cluster-scoped object",
			err:    errors.New("example error"),
			id:     namespaceObjID,
			object: namespaceObj,
			expectedMessage: "KNV2009: failed to apply Namespace, /test-namespace: example error\n\n" +
				"metadata.name: test-namespace\n" +
				"group:\n" +
				"version: v1\n" +
				"kind: Namespace\n\n" +
				"For more information, see https://g.co/cloud/acm-errors#knv2009",
			expectedCSE: v1beta1.ConfigSyncError{
				Code: ApplierErrorCode,
				ErrorMessage: "KNV2009: failed to apply Namespace, /test-namespace: example error\n\n" +
					"metadata.name: test-namespace\n" +
					"group:\n" +
					"version: v1\n" +
					"kind: Namespace\n\n" +
					"For more information, see https://g.co/cloud/acm-errors#knv2009",
				Resources: []v1beta1.ResourceRef{
					{
						Name: "test-namespace",
						GVK: metav1.GroupVersionKind{
							Version: "v1",
							Kind:    "Namespace",
						},
					},
				},
			},
		},
		{
			name:   "object with source path",
			err:    errors.New("another error"),
			id:     anvilObjID,
			object: anvilObj,
			expectedMessage: "KNV2009: failed to apply Anvil.acme.com, test-namespace/test-anvil: another error\n\n" +
				"source: foo/anvil.yaml\n" +
				"namespace: test-namespace\n" +
				"metadata.name: test-anvil\n" +
				"group: acme.com\n" +
				"version: v1\n" +
				"kind: Anvil\n\n" +
				"For more information, see https://g.co/cloud/acm-errors#knv2009",
			expectedCSE: v1beta1.ConfigSyncError{
				Code: ApplierErrorCode,
				ErrorMessage: "KNV2009: failed to apply Anvil.acme.com, test-namespace/test-anvil: another error\n\n" +
					"source: foo/anvil.yaml\n" +
					"namespace: test-namespace\n" +
					"metadata.name: test-anvil\n" +
					"group: acme.com\n" +
					"version: v1\n" +
					"kind: Anvil\n\n" +
					"For more information, see https://g.co/cloud/acm-errors#knv2009",
				Resources: []v1beta1.ResourceRef{
					{
						SourcePath: "foo/anvil.yaml",
						Name:       "test-anvil",
						Namespace:  "test-namespace",
						GVK: metav1.GroupVersionKind{
							Group:   "acme.com",
							Version: "v1",
							Kind:    "Anvil",
						},
					},
				},
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := ErrorForResourceWithResource(tc.err, tc.id, tc.object)
			testutil.AssertEqual(t, tc.expectedMessage, err.Error())
			testutil.AssertEqual(t, tc.expectedCSE, err.ToCSE())
		})
	}
}
