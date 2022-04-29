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

// Package metadata provides validation checks for errors in Resource metadata
//
// These checks are specifically on the `metadata` fields, which are defined for all Resources.
// Validators MAY be triggered by Group/Version/Kind, but SHOULD NOT access Kind-specific fields.
// Kinds with validation specific to their fields SHOULD have their own dedicated package.
package metadata
