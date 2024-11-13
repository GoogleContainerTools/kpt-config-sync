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

package reconciler

import (
	"time"

	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/syncclient"
)

// ReconcilerOptions holds configuration for the reconciler.
type ReconcilerOptions struct {
	// Extend parser options to ensure they're using the same dependencies.
	*syncclient.Options

	// Updater syncs the source from the parsed cache to the cluster.
	*parse.Updater

	// StatusUpdatePeriod is how long the Parser waits between updates of the
	// sync status, to account for management conflict errors from the Remediator.
	StatusUpdatePeriod time.Duration

	// RenderingEnabled indicates whether the hydration-controller is currently
	// running for this reconciler.
	RenderingEnabled bool
}
