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

package syncertest

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Token is a test sync token.
const Token = "b38239ea8f58eaed17af6734bd6a025eeafccda1"

var (
	// Converter is an unstructured.Unstructured converter used for testing.
	Converter = runtime.NewTestUnstructuredConverter(conversion.EqualitiesOrDie())

	// ManagementEnabled sets management labels and annotations on the object.
	ManagementEnabled core.MetaMutator = func(obj client.Object) {
		core.SetAnnotation(obj, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
		core.SetAnnotation(obj, metadata.ResourceIDKey, core.GKNN(obj))
		core.SetLabel(obj, metadata.ManagedByKey, metadata.ManagedByValue)
	}
	// ManagementDisabled sets the management disabled annotation on the object.
	ManagementDisabled = core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementDisabled)
	// ManagementInvalid sets an invalid management annotation on the object.
	ManagementInvalid = core.Annotation(metadata.ResourceManagementKey, "invalid")
	// TokenAnnotation sets the sync token annotation on the object
	TokenAnnotation = core.Annotation(metadata.SyncTokenAnnotationKey, Token)
)

// ToUnstructured converts the object to an unstructured.Unstructured.
func toUnstructured(t *testing.T, converter runtime.UnstructuredConverter, obj client.Object) *unstructured.Unstructured {
	if obj == nil {
		return &unstructured.Unstructured{}
	}
	u, err := converter.ToUnstructured(obj)
	// We explicitly remove the status field from objects during reconcile. So,
	// we need to do the same for test objects we convert to unstructured.Unstructured.
	unstructured.RemoveNestedField(u, "status")
	if err != nil {
		t.Fatalf("could not convert to unstructured type: %#v", obj)
	}
	return &unstructured.Unstructured{Object: u}
}

// ToUnstructuredList converts the objects to an unstructured.UnstructedList.
func ToUnstructuredList(t *testing.T, converter runtime.UnstructuredConverter, objs ...client.Object) []*unstructured.Unstructured {
	result := make([]*unstructured.Unstructured, len(objs))
	for i, obj := range objs {
		result[i] = toUnstructured(t, converter, obj)
	}
	return result
}

// ClusterConfigImportToken adds an import token to a ClusterConfig.
func ClusterConfigImportToken(t string) fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		cc.Spec.Token = t
	}
}

// ClusterConfigImportTime adds an ImportTime to a ClusterConfig.
func ClusterConfigImportTime(time metav1.Time) fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		cc.Spec.ImportTime = time
	}
}

// ClusterConfigSyncTime adds a SyncTime to a ClusterConfig.
func ClusterConfigSyncTime() fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		cc.Status.SyncTime = Now()
	}
}

// ClusterConfigSyncToken adds a sync token to a ClusterConfig.
func ClusterConfigSyncToken() fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		cc.Status.Token = Token
	}
}

// NamespaceConfigImportToken adds an import token to a Namespace Config.
func NamespaceConfigImportToken(t string) core.MetaMutator {
	return func(o client.Object) {
		nc := o.(*v1.NamespaceConfig)
		nc.Spec.Token = t
	}
}

// NamespaceConfigImportTime adds an ImportTime to a Namespace Config.
func NamespaceConfigImportTime(time metav1.Time) core.MetaMutator {
	return func(o client.Object) {
		nc := o.(*v1.NamespaceConfig)
		nc.Spec.ImportTime = time
	}
}

// NamespaceConfigSyncTime adds a sync time to a Namespace Config.
func NamespaceConfigSyncTime() core.MetaMutator {
	return func(o client.Object) {
		nc := o.(*v1.NamespaceConfig)
		nc.Status.SyncTime = Now()
	}
}

// NamespaceConfigSyncToken adds a sync token to a Namespace Config.
func NamespaceConfigSyncToken() core.MetaMutator {
	return func(o client.Object) {
		nc := o.(*v1.NamespaceConfig)
		nc.Status.Token = Token
	}
}

// MarkForDeletion marks a NamespaceConfig with an intent to be delete
func MarkForDeletion() core.MetaMutator {
	return func(o client.Object) {
		nc := o.(*v1.NamespaceConfig)
		nc.Spec.DeleteSyncedTime = metav1.Now()
	}
}

// Now returns a stubbed time, at epoch.
func Now() metav1.Time {
	return metav1.Time{Time: time.Unix(0, 0)}
}
