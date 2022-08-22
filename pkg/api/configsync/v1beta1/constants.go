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

package v1beta1

import (
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// SyncFinalizer is a finalizer handled by ReconcilerManager to ensure
// deletion of managed resource objects complete before the RootSync/RepoSync is
// deleted
const SyncFinalizer = reconcilermanager.ManagerName + "." + configmanagement.GroupName
