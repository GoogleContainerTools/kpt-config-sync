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

// Package meta includes controllers responsible for managing other controllers based on Syncs and CRDs.
package meta

import (
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/syncer/crd"
	"kpt.dev/configsync/pkg/syncer/sync"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddControllers adds all controllers that manage other controllers.
func AddControllers(mgr manager.Manager) error {
	// Set up Scheme for nomos resources.
	if err := v1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	rc := sync.RestartChannel(make(chan event.GenericEvent))
	if err := sync.AddController(mgr, rc); err != nil {
		return err
	}

	return crd.AddCRDController(mgr, sync.RestartSignal(rc))
}
