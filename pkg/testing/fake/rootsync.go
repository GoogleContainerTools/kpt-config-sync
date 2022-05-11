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

package fake

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RootSyncObjectV1Alpha1 initializes a RootSync.
func RootSyncObjectV1Alpha1(name string, opts ...core.MetaMutator) *v1alpha1.RootSync {
	result := &v1alpha1.RootSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: configsync.ControllerNamespace,
		},
		TypeMeta: ToTypeMeta(kinds.RootSyncV1Alpha1()),
	}
	mutate(result, opts...)

	return result
}

// RootSyncObjectV1Beta1 initializes a RootSync with version v1beta1.
func RootSyncObjectV1Beta1(name string, opts ...core.MetaMutator) *v1beta1.RootSync {
	result := &v1beta1.RootSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: configsync.ControllerNamespace,
		},
		TypeMeta: ToTypeMeta(kinds.RootSyncV1Beta1()),
	}
	mutate(result, opts...)

	return result
}

// WithRootSyncSourceType sets the sourceType of the RootSync object.
func WithRootSyncSourceType(sourceType v1beta1.SourceType) core.MetaMutator {
	return func(o client.Object) {
		rs := o.(*v1beta1.RootSync)
		rs.Spec.SourceType = string(sourceType)
	}
}
