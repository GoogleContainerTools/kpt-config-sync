// Copyright 2023 Google LLC
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

package testkubeclient

import (
	"kpt.dev/configsync/pkg/core"

	"kpt.dev/configsync/pkg/api/configsync"
)

// TestLabel is the label added to all test objects, ensuring we can clean up
// non-ephemeral clusters when tests are complete.
const TestLabel = "nomos-test"

// TestLabelValue is the value assigned to the above label.
const TestLabelValue = "enabled"

// AddTestLabel is automatically added to objects created or declared with the
// NT methods, or declared with Repository.Add.
//
// This isn't perfect - objects added via other means (such as kubectl) will
// bypass this.
var AddTestLabel = core.Label(TestLabel, TestLabelValue)

// FieldManager is the field manager to use when creating, updating, and
// patching kubernetes objects with kubectl and client-go. This is used to
// uniquely identify the nomostest client, to enable field pruning and merging.
// This must be different from the field manager used by config sync, in order
// to allow both clients to manage different fields on the same objects.
const FieldManager = configsync.GroupName + "/nomostest"
