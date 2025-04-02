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

package parse

import (
	"encoding/json"
	"fmt"

	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/applyset"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
)

// sourceContext contains the fields which identify where a resource is being synced from.
type sourceContext struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch,omitempty"`
	Rev    string `json:"rev,omitempty"`
}

func addAnnotationsAndLabels(objs []ast.FileObject, scope declared.Scope, syncName string, sc sourceContext, commitHash string) error {
	gcVal, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("marshaling sourceContext: %w", err)
	}
	csm := metadata.ConfigSyncMetadata{
		ApplySetID:      applyset.IDFromSync(syncName, scope),
		GitContextValue: string(gcVal),
		ManagerValue:    declared.ResourceManager(scope, syncName),
		SourceHash:      commitHash,
		InventoryID:     applier.InventoryID(syncName, scope.SyncNamespace()),
	}
	for _, obj := range objs {
		csm.SetConfigSyncMetadata(obj)
	}
	return nil
}
