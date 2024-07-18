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

package validate

import (
	"errors"
	"testing"

	"k8s.io/api/extensions/v1beta1"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
)

func TestDeprecatedKinds(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Non-deprecated Deployment",
			obj:  k8sobjects.Deployment("namespaces/foo"),
		},
		{
			name:    "Deprecated Deployment",
			obj:     k8sobjects.Unstructured(v1beta1.SchemeGroupVersion.WithKind("Deployment")),
			wantErr: status.FakeError(nonhierarchical.DeprecatedGroupKindErrorCode),
		},
		{
			name:    "Deprecated PodSecurityPolicy",
			obj:     k8sobjects.Unstructured(v1beta1.SchemeGroupVersion.WithKind("PodSecurityPolicy")),
			wantErr: status.FakeError(nonhierarchical.DeprecatedGroupKindErrorCode),
		},
		{
			name:    "Deprecated Ingress",
			obj:     k8sobjects.Unstructured(v1beta1.SchemeGroupVersion.WithKind("Ingress")),
			wantErr: status.FakeError(nonhierarchical.DeprecatedGroupKindErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := DeprecatedKinds(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got DeprecatedKinds() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
