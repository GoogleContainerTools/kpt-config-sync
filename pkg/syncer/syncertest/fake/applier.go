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

// Applier implements a fake reconcile.Applier for use in testing.
//
// reconcile.Applier not imported due to import cycle considerations.
type Applier struct {
	Client      *Client
	CreateError status.Error
	UpdateError status.Error
	DeleteError status.Error
}

var _ reconcile.Applier = &Applier{}

// Create implements reconcile.Applier.
func (a *Applier) Create(ctx context.Context, obj *unstructured.Unstructured) status.Error {
	if a.CreateError != nil {
		return a.CreateError
	}
	err := a.Client.Create(ctx, obj)
	if err != nil {
		return status.APIServerError(err, "creating")
	}
	return nil
}

// Update implements reconcile.Applier.
func (a *Applier) Update(ctx context.Context, intendedState, _ *unstructured.Unstructured) status.Error {
	if a.UpdateError != nil {
		return a.UpdateError
	}
	err := a.Client.Update(ctx, intendedState)
	if err != nil {
		return status.APIServerError(err, "updating")
	}
	return nil
}

// RemoveNomosMeta implements reconcile.Applier.
func (a *Applier) RemoveNomosMeta(ctx context.Context, intent *unstructured.Unstructured, _ string) status.Error {
	updated := metadata.RemoveConfigSyncMetadata(intent)
	if !updated {
		return nil
	}

	err := a.Client.Update(ctx, intent)
	if err != nil {
		return status.APIServerError(err, "removing meta")
	}
	return nil
}

// Delete implements reconcile.Applier.
func (a *Applier) Delete(ctx context.Context, obj *unstructured.Unstructured) status.Error {
	if a.DeleteError != nil {
		return a.DeleteError
	}
	err := a.Client.Delete(ctx, obj)
	if err != nil {
		return status.APIServerError(err, "deleting")
	}
	return nil
}

// GetClient implements reconcile.Applier.
func (a *Applier) GetClient() client.Client {
	return a.Client
}
