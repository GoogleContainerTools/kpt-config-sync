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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/lifecycle"
	"kpt.dev/configsync/pkg/metadata"
)

// NamespaceDiff represents a diff between a Namespace config and the one on the cluster.
type NamespaceDiff struct {
	Name     string
	Declared *v1.NamespaceConfig
	Actual   *corev1.Namespace
}

// Type returns the type of the NamespaceDiff.
// TODO: Merge NamespaceDiff with Diff since there's overlap.
func (d *NamespaceDiff) Type() Type {

	if d.Declared != nil {
		// The NamespaceConfig IS on the cluster.

		if !d.Declared.Spec.DeleteSyncedTime.IsZero() {
			// NamespaceConfig is marked for deletion
			if d.Actual == nil || ManagementDisabled(d.Declared) {
				// The corresponding namespace has already been deleted or the namespace is explicitly marked management disabled in the repository, so delete the NsConfig.
				return DeleteNsConfig
			}
			if lifecycle.HasPreventDeletion(d.Actual) || IsManageableSystemNamespace(d.Actual) {
				return UnmanageNamespace
			}
			return Delete
		}

		if ManagementUnset(d.Declared) {
			// The declared Namespace has no resource management key, so it is managed.
			if d.Actual != nil {
				// The Namespace is also in the cluster, so update it.
				return Update
			}

			// The Namespace is not in the cluster, so create it.
			return Create
		}
		if ManagementDisabled(d.Declared) {
			// The Namespace is explicitly marked management disabled in the repository.
			if d.Actual != nil {
				if metadata.HasConfigSyncMetadata(d.Actual) {
					// Management is disabled for the Namespace, so remove management annotations from the API Server.
					return Unmanage
				}
			}
			// Management disabled and there's no required changes to the Namespace.
			return NoOp
		}
		// The management annotation in the repo is invalid, so show an error.
		return Error
	}

	// The NamespaceConfig IS NOT in the cluster.
	if d.Actual != nil && ManagedByConfigSync(d.Actual) {
		// d.Actual is managed by Config Sync.
		//
		// This is a strange case to arrive at. A user would have to have a managed namespace,
		// uninstall Nomos, remove the declaration of the namespace from the repo, then reinstall
		// Nomos with the actual namespace still present and annotated from when it was managed. We
		// can't infer the user's intent so we just NoOp.
		klog.Warningf("Ignoring Namespace %q which has management annotations but there is no NamespaceConfig.", d.Name)
	}

	// The Namespace does not exist on the API Server and has no corresponding NamespaceConfig, so do nothing.
	return NoOp
}
