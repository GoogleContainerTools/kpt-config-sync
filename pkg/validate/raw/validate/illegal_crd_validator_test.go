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

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestIllegalCRD(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Anvil v1beta1 CRD",
			obj:  crdv1beta1("crd", kinds.Anvil()),
		},
		{
			name:    "ClusterConfig v1beta1 CRD",
			obj:     crdv1beta1("crd", kinds.ClusterConfig()),
			wantErr: fake.Error(nonhierarchical.UnsupportedObjectErrorCode),
		},
		{
			name:    "ClusterConfig v1 CRD",
			obj:     crdv1("crd", kinds.ClusterConfig()),
			wantErr: fake.Error(nonhierarchical.UnsupportedObjectErrorCode),
		},
		{
			name:    "RepoSync v1beta1 CRD",
			obj:     crdv1beta1("crd", kinds.RepoSyncV1Beta1()),
			wantErr: fake.Error(nonhierarchical.UnsupportedObjectErrorCode),
		},
		{
			name:    "RepoSync v1 CRD",
			obj:     crdv1("crd", kinds.RepoSyncV1Beta1()),
			wantErr: fake.Error(nonhierarchical.UnsupportedObjectErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := IllegalCRD(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got IllegalCRD() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
