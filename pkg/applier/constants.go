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

package applier

import "kpt.dev/configsync/pkg/api/configsync"

const (
	// StatusEnabled is used to allow kpt applier to inject the actuation status
	// into the ResourceGroup object.
	StatusEnabled = "enabled"
	//  StatusDisabled is used to stop kpt applier to inject the actuation status
	// into the ResourceGroup object.
	StatusDisabled = "disabled"

	// StatusModeKey annotates a ResourceGroup CR
	// to communicate with the ResourceGroup controller.
	// When the value is set to "disabled", the ResourceGroup controller
	// ignores the ResourceGroup CR.
	StatusModeKey = configsync.ConfigSyncPrefix + "status"
)
