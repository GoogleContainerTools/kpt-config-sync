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

package fake

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// applier implements a fake reconcile.Applier for use in testing.
//
// reconcile.Applier not imported due to import cycle considerations.
type applier struct {
	*Client
}

var _ reconcile.Applier = &applier{}

// Create implements reconcile.Applier.
func (a *applier) Create(ctx context.Context, obj *unstructured.Unstructured) (bool, status.Error) {
	err := a.Client.Create(ctx, obj)
	if err != nil {
		return false, status.APIServerError(err, "creating")
	}
	return true, nil
}

// Update implements reconcile.Applier.
func (a *applier) Update(ctx context.Context, intendedState, _ *unstructured.Unstructured) (bool, status.Error) {
	err := a.Client.Update(ctx, intendedState)
	if err != nil {
		return false, status.APIServerError(err, "updating")
	}
	return true, nil
}

// RemoveNomosMeta implements reconcile.Applier.
func (a *applier) RemoveNomosMeta(ctx context.Context, intent *unstructured.Unstructured) (bool, status.Error) {
	updated := metadata.RemoveConfigSyncMetadata(intent)
	if !updated {
		return false, nil
	}

	err := a.Client.Update(ctx, intent)
	if err != nil {
		return false, status.APIServerError(err, "removing meta")
	}
	return true, nil
}

// Delete implements reconcile.Applier.
func (a *applier) Delete(ctx context.Context, obj *unstructured.Unstructured) (bool, status.Error) {
	err := a.Client.Delete(ctx, obj)
	if err != nil {
		return false, status.APIServerError(err, "deleting")
	}
	return true, nil
}

// GetClient implements reconcile.Applier.
func (a *applier) GetClient() client.Client {
	return a.Client
}
