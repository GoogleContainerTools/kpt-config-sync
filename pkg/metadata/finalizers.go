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

package metadata

import (
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

const (
	// ReconcilerFinalizer is the finalizer added to the RootSync/RepoSync by
	// the reconciler when the deletion-propagation-policy is Foreground to
	// ensure deletion of the user objects it manages, before the
	// RootSync/RepoSync is deleted.
	ReconcilerFinalizer = configsync.ConfigSyncPrefix + reconcilermanager.Reconciler

	// ReconcilerManagerFinalizer is the finalizer added to the
	// RootSync/RepoSync by the reconciler-manager to ensure
	// deletion of the reconciler and its dependencies, before the
	// RootSync/RepoSync is deleted.
	ReconcilerManagerFinalizer = configsync.ConfigSyncPrefix + reconcilermanager.ManagerName
)
