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

package rootsync

import (
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectKey returns a key appropriate for fetching a RootSync.
func ObjectKey(name string) client.ObjectKey {
	return client.ObjectKey{
		Namespace: configsync.ControllerNamespace,
		Name:      name,
	}
}

// GetHelmBase returns the spec.helm.helmBase when spec.helm is not nil
func GetHelmBase(helm *v1beta1.HelmRootSync) *v1beta1.HelmBase {
	if helm == nil {
		return nil
	}
	return &helm.HelmBase
}
