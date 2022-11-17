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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
)

// DynamicClient is a fake dynamic client which contains a map to store unstructured objects
type DynamicClient struct {
	*dynamicfake.FakeDynamicClient
	UnObjects map[core.ID]*unstructured.Unstructured
}

// NewDynamicClient instantiates a new fake.DynamicClient
func NewDynamicClient(t *testing.T, scheme *runtime.Scheme) *DynamicClient {
	t.Helper()

	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	unObjs := make(map[core.ID]*unstructured.Unstructured)

	fakeClient.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		gk := kinds.Deployment().GroupKind()
		name := action.(clienttesting.GetAction).GetName()
		namespace := action.(clienttesting.GetAction).GetNamespace()
		id := genID(namespace, name, gk)
		if unObjs[id] != nil {
			return true, unObjs[id].DeepCopy(), nil
		}
		return false, nil, nil
	})
	fakeClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchData := action.(clienttesting.PatchAction).GetPatch()
		var u unstructured.Unstructured
		gk := kinds.Deployment().GroupKind()
		name := action.(clienttesting.PatchAction).GetName()
		namespace := action.(clienttesting.PatchAction).GetNamespace()
		id := genID(namespace, name, gk)
		if err := u.UnmarshalJSON(patchData); err != nil {
			return false, nil, nil
		}
		unObjs[id] = &u
		return true, unObjs[id].DeepCopy(), nil
	})
	fakeClient.PrependReactor("delete", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		gk := kinds.Deployment().GroupKind()
		name := action.(clienttesting.DeleteAction).GetName()
		namespace := action.(clienttesting.DeleteAction).GetNamespace()
		id := genID(namespace, name, gk)
		if unObjs[id] != nil {
			delete(unObjs, id)
			return true, nil, nil
		}
		return false, nil, nil
	})

	return &DynamicClient{
		fakeClient, unObjs,
	}
}

func genID(namespace, name string, gk schema.GroupKind) core.ID {
	return core.ID{
		GroupKind: gk,
		ObjectKey: types.NamespacedName{Namespace: namespace, Name: name},
	}
}
