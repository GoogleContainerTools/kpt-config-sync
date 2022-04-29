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

package reconcile

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pkg/errors"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/status"
)

func TestFilterContextCancelled(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "error with no cause",
			err:  fmt.Errorf("simple"),
			want: fmt.Errorf("simple"),
		},
		{
			name: "error with cancelled cause",
			err:  errors.Wrap(context.Canceled, "outer error"),
		},
		{
			name: "error with other cause",
			err:  errors.Wrap(errors.New("some cause"), "outer error"),
			want: errors.Wrap(errors.New("some cause"), "outer error"),
		},
		{
			name: "multiple errors with context cancelled",
			err:  status.Append(nil, errors.Wrap(context.Canceled, "outer error"), errors.Wrap(context.Canceled, "another error")),
		},

		{
			name: "one error with context cancelled, two not",
			err:  status.Append(nil, errors.Wrap(context.Canceled, "outer error"), errors.Wrap(errors.New("some cause"), "another error"), errors.New("some error")),
			want: status.Append(nil, errors.Wrap(errors.New("some cause"), "another error"), errors.New("some error")),
		},
		{
			name: "filter nested multi error",
			err: status.Append(nil,
				fmt.Errorf("no cause"),
				status.Append(nil, errors.Wrap(context.Canceled, "outer error"), errors.Wrap(context.Canceled, "another error")),
			),
			want: status.Append(nil, fmt.Errorf("no cause")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterContextCancelled(tc.err)

			if got == nil && tc.want == nil {
				return
			}
			if got == nil || tc.want == nil || got.Error() != tc.want.Error() {
				t.Errorf("filtered error is unexpected, got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

func TestPendingReconiclerRestart(t *testing.T) {
	testCases := []struct {
		name    string
		resGks  []schema.GroupKind
		toSync  []schema.GroupVersionKind
		wantRet []string
	}{
		{
			name:    "empty",
			resGks:  []schema.GroupKind{},
			toSync:  []schema.GroupVersionKind{},
			wantRet: nil,
		},
		{
			name: "ok - covered",
			resGks: []schema.GroupKind{
				{Kind: "ConfigMap"},
			},
			toSync: []schema.GroupVersionKind{
				{Kind: "ConfigMap", Version: "v1"},
				{Kind: "ConfigMap", Version: "v1beta1"},
			},
			wantRet: nil,
		},
		{
			name:   "ok - sync but no res",
			resGks: []schema.GroupKind{},
			toSync: []schema.GroupVersionKind{
				{Kind: "ConfigMap", Version: "v1"},
				{Kind: "ConfigMap", Version: "v1beta1"},
			},
			wantRet: nil,
		},
		{
			name: "need restart",
			resGks: []schema.GroupKind{
				{Kind: "ConfigMap"},
			},
			toSync:  []schema.GroupVersionKind{},
			wantRet: []string{"ConfigMap"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var resources []v1.GenericResources
			for _, gk := range tc.resGks {
				resources = append(resources, v1.GenericResources{
					Group: gk.Group,
					Kind:  gk.Kind,
				})
			}

			got := resourcesWithoutSync(resources, tc.toSync)
			if d := cmp.Diff(got, tc.wantRet); d != "" {
				t.Errorf("Wanted %v, got %v, diff: %s", tc.wantRet, got, d)
			}
		})
	}
}
