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

package differ

import (
	"context"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/util/compare"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Alias metav1.Now to enable test mocking.
var now = metav1.Now

// Update updates the nomos CRs on the cluster based on the difference between the desired and current state.
// initTime is the importer-reconciler's instantiation time. It is used to update the stalled import time of
// ClusterConfigs and NamespaceConfigs so syncer-reconcilers can force-update everything on startup.
func Update(ctx context.Context, client *syncerclient.Client, decoder decode.Decoder, current, desired namespaceconfig.AllConfigs, initTime time.Time) status.MultiError {
	var errs status.MultiError
	impl := &differ{client: client, errs: errs}
	impl.updateNamespaceConfigs(ctx, decoder, current, desired, initTime)
	impl.updateClusterConfigs(ctx, decoder, current, desired, initTime)
	impl.updateSyncs(ctx, current, desired)
	return impl.errs
}

// differ does the actual update operations on the cluster and keeps track of the errors encountered while doing so.
type differ struct {
	client *syncerclient.Client
	errs   status.MultiError
}

// namespaceConfigNeedsUpdate checks if the NamespaceConfig resource needs an update.
// initTime is the importer-reconciler's instantiation time. It is used to update
// the stalled import time of ClusterConfigs and NamespaceConfigs so syncer-reconcilers
// can force-update everything on startup.
// An update is needed if NamespaceConfigs are not equivalent or `.spec.ImportTime`
// is stale (not after importer-reconciler's init time or not after deleteSyncedTime).
func (d *differ) namespaceConfigNeedsUpdate(decoder decode.Decoder, intent, actual *v1.NamespaceConfig, initTime time.Time) bool {
	equal, err := namespaceconfig.NamespaceConfigsEqual(decoder, intent, actual)
	if err != nil {
		d.errs = status.Append(d.errs, err)
		return false
	}
	return !equal || !actual.Spec.ImportTime.After(initTime) || !actual.Spec.ImportTime.After(actual.Spec.DeleteSyncedTime.Time)
}

// updateNamespaceConfigs compares the given sets of current and desired NamespaceConfigs and performs the necessary
// actions to update the current set to match the desired set.
// initTime is the importer-reconciler's instantiation time. It is used to update the stalled import time of
// ClusterConfigs and NamespaceConfigs so syncer-reconcilers can force-update everything on startup.
func (d *differ) updateNamespaceConfigs(ctx context.Context, decoder decode.Decoder, current, desired namespaceconfig.AllConfigs, initTime time.Time) {
	var deletes, creates, updates int
	for name := range desired.NamespaceConfigs {
		intent := desired.NamespaceConfigs[name]
		if actual, found := current.NamespaceConfigs[name]; found {
			if d.namespaceConfigNeedsUpdate(decoder, &intent, &actual, initTime) {
				err := d.updateNamespaceConfig(ctx, &intent)
				d.errs = status.Append(d.errs, err)
				updates++
			}
		} else {
			d.errs = status.Append(d.errs, d.client.Create(ctx, &intent))
			creates++
		}
	}
	for name, mayDelete := range current.NamespaceConfigs {
		if _, found := desired.NamespaceConfigs[name]; !found {
			d.errs = status.Append(d.errs, d.deleteNamespaceConfig(ctx, &mayDelete))
			deletes++
		}
	}

	klog.Infof("NamespaceConfig operations: create %d, update %d, delete %d", creates, updates, deletes)
}

// deleteNamespaceConfig marks the given NamespaceConfig for deletion by the syncer. This tombstone is more explicit
// than having the importer just delete the NamespaceConfig directly.
func (d *differ) deleteNamespaceConfig(ctx context.Context, nc *v1.NamespaceConfig) status.Error {
	_, err := d.client.Update(ctx, nc, func(obj client.Object) (client.Object, error) {
		newObj := obj.(*v1.NamespaceConfig).DeepCopy()
		newObj.Spec.DeleteSyncedTime = now()
		return newObj, nil
	})
	return err
}

// updateNamespaceConfig writes the given NamespaceConfig to storage as it is specified.
func (d *differ) updateNamespaceConfig(ctx context.Context, intent *v1.NamespaceConfig) status.Error {
	_, err := d.client.Update(ctx, intent, func(obj client.Object) (client.Object, error) {
		oldObj := obj.(*v1.NamespaceConfig)
		newObj := intent.DeepCopy()
		if !oldObj.Spec.DeleteSyncedTime.IsZero() {
			e := status.ResourceWrap(
				errors.Errorf("NamespaceConfig %s has .spec.deleteSyncedTime set, cannot update", oldObj.Name),
				"", oldObj)
			return nil, e
		}
		newObj.ResourceVersion = oldObj.ResourceVersion
		return newObj, nil
	})
	if err != nil {
		return err
	}

	_, err = d.client.UpdateStatus(ctx, intent, func(obj client.Object) (client.Object, error) {
		oldObj := obj.(*v1.NamespaceConfig)
		newObj := intent.DeepCopy()
		newObj.ResourceVersion = oldObj.ResourceVersion
		newSyncState := newObj.Status.SyncState
		oldObj.Status.DeepCopyInto(&newObj.Status)
		if !newSyncState.IsUnknown() {
			newObj.Status.SyncState = newSyncState
		}
		return newObj, nil
	})
	return err
}

func (d *differ) updateClusterConfigs(ctx context.Context, decoder decode.Decoder, current, desired namespaceconfig.AllConfigs, initTime time.Time) {
	d.updateClusterConfig(ctx, decoder, current.ClusterConfig, desired.ClusterConfig, initTime)
	d.updateClusterConfig(ctx, decoder, current.CRDClusterConfig, desired.CRDClusterConfig, initTime)
}

// clusterConfigNeedsUpdate checks if the ClusterConfig resource needs an update.
// An update is needed if ClusterConfigs are not equivalent or `.spec.ImportTime` is stale (not after importer-reconciler's init time).
// initTime is the importer-reconciler's instantiation time. It is used to update the stalled import time of
// ClusterConfigs and NamespaceConfigs so syncer-reconcilers can force-update everything on startup.
func (d *differ) clusterConfigNeedsUpdate(decoder decode.Decoder, current, desired *v1.ClusterConfig, initTime time.Time) bool {
	equal, err := compare.GenericResourcesEqual(decoder, desired.Spec.Resources, current.Spec.Resources)
	if err != nil {
		d.errs = status.Append(d.errs, err)
		return false
	}
	return !equal || !current.Spec.ImportTime.After(initTime)
}

// updateClusterConfig compares the given sets of current and desired ClusterConfigs and performs the necessary
// actions to update the current set to match the desired set.
// initTime is the importer-reconciler's instantiation time. It is used to update the stalled import time of
// ClusterConfigs and NamespaceConfigs so syncer-reconcilers can force-update everything on startup.
func (d *differ) updateClusterConfig(ctx context.Context, decoder decode.Decoder, current, desired *v1.ClusterConfig, initTime time.Time) {
	if current == nil && desired == nil {
		return
	}
	if current == nil {
		d.errs = status.Append(d.errs, d.client.Create(ctx, desired))
		return
	}
	if desired == nil {
		d.errs = status.Append(d.errs, d.client.Delete(ctx, current))
		return
	}

	if d.clusterConfigNeedsUpdate(decoder, current, desired, initTime) {
		_, err := d.client.Update(ctx, desired, func(obj client.Object) (client.Object, error) {
			oldObj := obj.(*v1.ClusterConfig)
			newObj := desired.DeepCopy()
			newObj.ResourceVersion = oldObj.ResourceVersion
			return newObj, nil
		})
		d.errs = status.Append(d.errs, err)

		_, err = d.client.UpdateStatus(ctx, desired, func(obj client.Object) (client.Object, error) {
			oldObj := obj.(*v1.ClusterConfig)
			newObj := desired.DeepCopy()
			newObj.ResourceVersion = oldObj.ResourceVersion
			newSyncState := newObj.Status.SyncState
			oldObj.Status.DeepCopyInto(&newObj.Status)
			if !newSyncState.IsUnknown() {
				newObj.Status.SyncState = newSyncState
			}
			return newObj, nil
		})
		d.errs = status.Append(d.errs, err)
	}
}

func (d *differ) updateSyncs(ctx context.Context, current, desired namespaceconfig.AllConfigs) {
	var creates, updates, deletes int
	for name, newSync := range desired.Syncs {
		if oldSync, exists := current.Syncs[name]; exists {
			if !syncsEqual(&newSync, &oldSync) {
				_, err := d.client.Update(ctx, &newSync, func(obj client.Object) (client.Object, error) {
					oldObj := obj.(*v1.Sync)
					newObj := newSync.DeepCopy()
					newObj.ResourceVersion = oldObj.ResourceVersion
					return newObj, nil
				})
				d.errs = status.Append(d.errs, err)
				updates++
			}
		} else {
			d.errs = status.Append(d.errs, d.client.Create(ctx, &newSync))
			creates++
		}
	}

	for name, mayDelete := range current.Syncs {
		if _, found := desired.Syncs[name]; !found {
			d.errs = status.Append(d.errs, d.client.Delete(ctx, &mayDelete))
			deletes++
		}
	}

	klog.Infof("Sync operations: %d updates, %d creates, %d deletes", updates, creates, deletes)
}

// syncsEqual returns true if the syncs are equivalent.
func syncsEqual(l *v1.Sync, r *v1.Sync) bool {
	return cmp.Equal(l.Spec, r.Spec) && compare.ObjectMetaEqual(l, r)
}
