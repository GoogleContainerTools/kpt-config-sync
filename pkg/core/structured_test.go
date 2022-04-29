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

package core_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestRemarshalToStructured(t *testing.T) {
	testcases := []struct {
		name string
		u    *unstructured.Unstructured
		obj  runtime.Object
	}{
		{
			name: "v1alpha1 RepoSync",
			u:    fake.UnstructuredObject(kinds.RepoSyncV1Alpha1(), core.Name(configsync.RepoSyncName), core.Namespace("test"), core.Annotations(nil), core.Labels(nil)),
			obj:  fake.RepoSyncObjectV1Alpha1("test", configsync.RepoSyncName),
		},
		{
			name: "v1beta1 RepoSync",
			u:    fake.UnstructuredObject(kinds.RepoSyncV1Beta1(), core.Name(configsync.RepoSyncName), core.Namespace("test"), core.Annotations(nil), core.Labels(nil)),
			obj:  fake.RepoSyncObjectV1Beta1("test", configsync.RepoSyncName),
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := core.RemarshalToStructured(tc.u)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(actual, tc.obj); diff != "" {
				t.Error(diff)
			}
		})
	}
}
