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

package controllers

// reconciler controller names
const (
	// SyncerController is the name of the syncer controller running in the reconciler container.
	// It is named syncer instead of parser because it includes parsing, validating and applying.
	// Also, there is already a Parser struct declared in pkg/parse/opts.go.
	SyncerController = "Syncer"
	// RemediatorController is the name of the remediator controller running in the reconciler container.
	RemediatorController = "Remediator"
)
