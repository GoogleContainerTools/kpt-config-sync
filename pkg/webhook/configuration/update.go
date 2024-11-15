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

package configuration

import (
	"context"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Update modifies the ValidatingWebhookConfiguration on the cluster to match
// all types declared in objs.
//
// Returns an error if the API Server returns invalid API Resource lists or
// there is a problem updating the Configuration.
func Update(ctx context.Context, c client.Client, dc discovery.ServerResourcer, gvks []schema.GroupVersionKind, opts ...client.UpdateOption) status.MultiError {
	if len(gvks) == 0 {
		// Nothing to do.
		return nil
	}

	_, _, err := dc.ServerGroupsAndResources()
	if err != nil {
		// Likely transient. If not, there's either a serious bug in our code or
		// something very wrong with the API Server.
		return status.APIServerError(err, "unable to list API Resources")
	}

	newCfg := toWebhookConfiguration(gvks)
	if newCfg == nil {
		// The repository declares no objects, so there's nothing to do.
		return nil
	}

	oldCfg := &admissionv1.ValidatingWebhookConfiguration{}
	err = c.Get(ctx, client.ObjectKey{Name: Name}, oldCfg)
	switch {
	case apierrors.IsNotFound(err):
		// The ACM operator is responsible to create the Config Sync ValidatingWebhookConfiguration object.
		// Two possibilities may cause the object not to be found:
		// 1) the ACM operator has not finished creating the object. In this case, as soon as the ValidatingWebhookConfiguration object is created successfully, a future reconciliation would update it.
		// 2) `ConfigManagement.Spec.PreventDrifts` is set to `false` in ACM 1.10.0+. In this case, the Config Sync reconciler does not need to update the ValidatingWebhookConfiguration object.
		return nil
	case err != nil:
		// Should be rare, but most likely will be a permission error.
		return status.APIServerError(err, "getting admission webhook from API Server")
	}

	// We aren't yet concerned with removing stale rules, so just merge the two
	// together.
	newCfg = Merge(oldCfg, newCfg)
	if err = c.Update(ctx, newCfg, opts...); err != nil {
		return status.APIServerError(err, "applying changes to admission webhook")
	}
	return nil
}
