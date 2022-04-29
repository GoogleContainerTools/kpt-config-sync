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

	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
)

// gitContext contains the fields which identify where a resource is being synced
// from.
type gitContext struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Rev    string `json:"rev"`
}

func addAnnotationsAndLabels(objs []ast.FileObject, scope declared.Scope, syncName string, gc gitContext, commitHash string) error {
	gcVal, err := json.Marshal(gc)
	if err != nil {
		return fmt.Errorf("marshaling gitContext: %w", err)
	}
	var inventoryID string
	if scope == declared.RootReconciler {
		inventoryID = applier.InventoryID(syncName, configmanagement.ControllerNamespace)
	} else {
		inventoryID = applier.InventoryID(syncName, string(scope))
	}
	for _, obj := range objs {
		core.SetLabel(obj, metadata.ManagedByKey, metadata.ManagedByValue)
		core.SetAnnotation(obj, metadata.GitContextKey, string(gcVal))
		core.SetAnnotation(obj, metadata.ResourceManagerKey, declared.ResourceManager(scope, syncName))
		core.SetAnnotation(obj, metadata.SyncTokenAnnotationKey, commitHash)
		core.SetAnnotation(obj, metadata.ResourceIDKey, core.GKNN(obj))
		core.SetAnnotation(obj, metadata.OwningInventoryKey, inventoryID)

		value := core.GetAnnotation(obj, metadata.ResourceManagementKey)
		if value != metadata.ResourceManagementDisabled {
			core.SetAnnotation(obj, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
		}
	}
	return nil
}
