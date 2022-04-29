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

package client_test

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	syncertestfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClient_Create(t *testing.T) {
	testCases := []struct {
		name     string
		declared client.Object
		client   client.Client
		wantErr  status.Error
	}{
		{
			name:     "Creates if does not exist",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client:   syncertestfake.NewClient(t, runtime.NewScheme()),
			wantErr:  nil,
		},
		{
			name:     "Retriable if receives AlreadyExists",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client: syncertestfake.NewClient(t, runtime.NewScheme(),
				fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			),
			wantErr: syncerclient.ConflictCreateAlreadyExists(errors.New("some error"),
				fake.RoleObject()),
		},
		{
			name:     "Generic APIServerError if other error",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client:   syncertestfake.NewErrorClient(errors.New("some API server error")),
			wantErr:  status.APIServerError(errors.New("some error"), "could not create"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sc := syncerclient.New(tc.client, nil)

			err := sc.Create(context.Background(), tc.declared)
			if !errors.Is(tc.wantErr, err) {
				t.Fatalf("got err %v, want err %v", err, tc.wantErr)
			}
		})
	}
}

func TestClient_Update(t *testing.T) {
	testCases := []struct {
		name     string
		declared client.Object
		client   client.Client
		wantErr  status.Error
	}{
		{
			name:     "Conflict error if not found",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client: syncertestfake.NewErrorClient(apierrors.NewNotFound(
				rbacv1.Resource("Role"), "admin")),
			wantErr: syncerclient.ConflictUpdateDoesNotExist(errors.New("not found"),
				fake.RoleObject(core.Name("admin"), core.Namespace("billing"))),
		},
		{
			name:     "Generic error if other error",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client:   syncertestfake.NewErrorClient(errors.New("some error")),
			wantErr: status.ResourceWrap(errors.New("some error"), "message",
				fake.RoleObject()),
		},
		{
			name:     "No error if client does not return error",
			declared: fake.RoleObject(core.Name("admin"), core.Namespace("billing")),
			client:   syncertestfake.NewErrorClient(nil),
			wantErr:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sc := syncerclient.New(tc.client, nil)

			_, err := sc.Update(context.Background(), tc.declared, noOpUpdate)
			if !errors.Is(tc.wantErr, err) {
				t.Fatalf("got err %v, want err %v", err, tc.wantErr)
			}
		})
	}
}

func noOpUpdate(o client.Object) (object client.Object, err error) { return o, nil }
