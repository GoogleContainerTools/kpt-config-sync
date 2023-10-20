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

package core

import (
	"fmt"
	"strings"

	"kpt.dev/configsync/pkg/api/configsync"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// NsReconcilerPrefix is the prefix used for all Namespace reconcilers.
	NsReconcilerPrefix = "ns-reconciler"
	// RootReconcilerPrefix is the prefix usef for all Root reconcilers.
	RootReconcilerPrefix = "root-reconciler"
)

var (
	// RootSyncPermissionsPrefix is the prefix used for all ClusterRoleBindings granting access to Root Reconcilers
	RootSyncPermissionsPrefix = fmt.Sprintf("%s:%s", configsync.RootSyncKind, RootReconcilerPrefix)
)

// RootReconcilerName returns the root reconciler's name in the format root-reconciler-<name>.
// If the RootSync name is "root-sync", it returns "root-reconciler" for backward compatibility.
func RootReconcilerName(name string) string {
	if name == configsync.RootSyncName {
		return RootReconcilerPrefix
	}
	return fmt.Sprintf("%s-%s", RootReconcilerPrefix, name)
}

// RootSyncName returns the RootSync's name given the name of its reconciler.
// It is the inverse of RootReconcilerName
func RootSyncName(reconcilerName string) string {
	if reconcilerName == RootReconcilerPrefix {
		return configsync.RootSyncName
	}

	syncName, _ := strings.CutPrefix(reconcilerName, fmt.Sprintf("%s-", RootReconcilerPrefix))
	return syncName
}

// NsReconcilerName returns the namespace reconciler's name in the format:
// ns-reconciler-<namespace>-<name>-<name-length>
// If the RepoSync name is "repo-sync", it returns "ns-reconciler-<namespace>" for backward compatibility.
func NsReconcilerName(namespace, name string) string {
	if name == configsync.RepoSyncName {
		return fmt.Sprintf("%s-%s", NsReconcilerPrefix, namespace)
	}
	return fmt.Sprintf("%s-%s-%s-%d", NsReconcilerPrefix, namespace, name, len(name))
}

// RootReconcilerObjectKey returns an ObjectKey for interacting with the
// RootReconciler for the specified RootSync.
func RootReconcilerObjectKey(syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      RootReconcilerName(syncName),
		Namespace: configsync.ControllerNamespace,
	}
}

// NsReconcilerObjectKey returns an ObjectKey for interracting with the
// NsReconciler for the specified RepoSync.
func NsReconcilerObjectKey(namespace, syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      NsReconcilerName(namespace, syncName),
		Namespace: configsync.ControllerNamespace,
	}
}
