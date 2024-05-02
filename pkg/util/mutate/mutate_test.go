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

package mutate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// decoder uses core.Scheme to parse YAML/JSON into typed objects
var decoder = serializer.NewCodecFactory(core.Scheme).UniversalDeserializer()

var deployment1Yaml = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello-world
  namespace: default
  uid: "1"
  resourceVersion: "1"
  generation: 1
spec:
  selector:
    matchLabels:
      app: hello-world
  replicas: 1
  template:
    metadata:
      labels:
        app: hello-world
    spec:
      containers:
      - name: hello
        image: "gcr.io/google-samples/hello-app:2.0"
        env:
        - name: "PORT"
          value: "50000"
`

func TestStatus(t *testing.T) {
	deployment1 := yamlToTypedObject(t, deployment1Yaml)

	var inputObj client.Object

	testCases := []struct {
		name            string
		obj             client.Object
		mutateFunc      Func
		existingObjs    []client.Object
		expectedUpdated bool
		expectedError   error
		expectedObj     client.Object
	}{
		{
			name: "no change client-side",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				return &NoUpdateError{}
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: false,
			expectedError:   nil,
			expectedObj:     deploymentCopy(deployment1), // ResourceVersion & generation unchanged
		},
		{
			name: "status change",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Status.Replicas = 1
				return nil
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: true,
			expectedError:   nil,
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				obj.Status.Replicas = 1
				obj.SetResourceVersion("2")
				// generation unchanged (no spec changes)
				return obj
			}(),
		},
		{
			name: "no change server-side",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Status.Replicas = 1
				obj.SetResourceVersion("1") // Fake a duplicate request
				return nil
			},
			existingObjs: []client.Object{
				func() client.Object {
					obj := deploymentCopy(deployment1)
					obj.Status.Replicas = 1
					obj.SetResourceVersion("2") // server object already updated
					// generation unchanged (no spec changes)
					return obj
				}(),
			},
			// Expect no update because there was an error
			expectedUpdated: false,
			// Expect a conflict, because the ResourceVersion in the request is older than the one on the server
			expectedError: status.APIServerErrorWrap(
				fmt.Errorf(
					"failed to update object status: %s: %w", kinds.ObjectSummary(deployment1),
					apierrors.NewConflict(
						schema.GroupResource{Group: "apps", Resource: "Deployment"},
						"default/hello-world",
						fmt.Errorf("ResourceVersion conflict: expected \"1\" but found \"2\"")),
				),
				deploymentCopy(deployment1)),
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				obj.Status.Replicas = 1
				obj.SetResourceVersion("2") // unchanged
				return obj
			}(),
		},
		{
			name: "ignore spec change",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Spec.Replicas = pointer.Int32(2)
				return nil
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: true,
			expectedError:   nil,
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				// spec change not persisted
				// generation unchanged (no changes)
				return obj
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// set the input object so tc.mutateFunc can update it
			inputObj = tc.obj

			scheme := core.Scheme
			fakeClient := fake.NewClient(t, scheme, tc.existingObjs...)
			ctx := context.Background()
			updated, err := Status(ctx, fakeClient, tc.obj, tc.mutateFunc)
			testerrors.AssertEqual(t, tc.expectedError, err)
			assert.Equal(t, tc.expectedUpdated, updated)
			fakeClient.Check(t, tc.expectedObj)
		})
	}
}

func TestSpec(t *testing.T) {
	deployment1 := yamlToTypedObject(t, deployment1Yaml)

	var inputObj client.Object
	var once sync.Once
	var fakeClient *fake.Client

	testCases := []struct {
		name            string
		obj             client.Object
		mutateFunc      Func
		existingObjs    []client.Object
		expectedUpdated bool
		expectedError   error
		expectedObj     client.Object
	}{
		{
			name: "no change client-side",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				return &NoUpdateError{}
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: false,
			expectedError:   nil,
			expectedObj:     deploymentCopy(deployment1), // ResourceVersion & generation unchanged
		},
		{
			name: "spec change",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Spec.Replicas = pointer.Int32(2)
				return nil
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: true,
			expectedError:   nil,
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				obj.Spec.Replicas = pointer.Int32(2)
				obj.SetResourceVersion("2") // updated
				obj.SetGeneration(2)        // updated
				return obj
			}(),
		},
		{
			name: "no change server-side (retry but eventually give up)",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Spec.Replicas = pointer.Int32(1)
				obj.SetResourceVersion("1") // Fake a duplicate request
				return nil
			},
			existingObjs: []client.Object{
				func() client.Object {
					obj := deploymentCopy(deployment1)
					obj.Spec.Replicas = pointer.Int32(1)
					obj.SetResourceVersion("2") // server object already updated
					obj.SetGeneration(2)        // server spec already updated
					return obj
				}(),
			},
			// Expect no update because there was an error
			expectedUpdated: false,
			// Expect a conflict, because the ResourceVersion in the request is older than the one on the server
			expectedError: status.APIServerErrorWrap(
				fmt.Errorf(
					"failed to update object: %s: %w", kinds.ObjectSummary(deployment1),
					apierrors.NewConflict(
						schema.GroupResource{Group: "apps", Resource: "Deployment"},
						"default/hello-world",
						fmt.Errorf("ResourceVersion conflict: expected \"1\" but found \"2\"")),
				),
				deploymentCopy(deployment1)),
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				obj.Spec.Replicas = pointer.Int32(1)
				obj.SetResourceVersion("2") // unchanged
				obj.SetGeneration(2)        // unchanged
				return obj
			}(),
		},
		{
			name: "ignore status change",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Status.Replicas = 1
				return nil
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			expectedUpdated: true,
			expectedError:   nil,
			expectedObj:     deploymentCopy(deployment1),
		},
		{
			name: "stale version (succeed on retry)",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)
				obj.Spec.Replicas = pointer.Int32(2)
				once.Do(func() {
					obj.SetResourceVersion("1") // Fake a stale request once
				})
				return nil
			},
			existingObjs: []client.Object{
				func() client.Object {
					obj := deploymentCopy(deployment1)
					obj.Spec.Replicas = pointer.Int32(1)
					obj.SetResourceVersion("2") // server object already updated
					obj.SetGeneration(2)        // server spec already updated
					return obj
				}(),
			},
			// Expect no update because there was an error
			expectedUpdated: true,
			// Expect a conflict, because the ResourceVersion in the request is older than the one on the server
			expectedError: nil,
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				obj.Spec.Replicas = pointer.Int32(2)
				obj.SetResourceVersion("3") // changed
				obj.SetGeneration(3)        // changed
				return obj
			}(),
		},
		{
			name: "async re-creation",
			obj:  deploymentCopy(deployment1),
			mutateFunc: func() error {
				obj := inputObj.(*appsv1.Deployment)

				// Fake async delete
				err := fakeClient.Delete(context.Background(), obj.DeepCopy())
				if err != nil {
					return fmt.Errorf("failed to delete object: %w", err)
				}
				// Fake async re-create (with different UID)
				replacementObj := obj.DeepCopy()
				replacementObj.SetUID("2")
				err = fakeClient.Create(context.Background(), replacementObj)
				if err != nil {
					return fmt.Errorf("failed to create object: %w", err)
				}

				// Continue mutation
				obj.Spec.Replicas = pointer.Int32(1)
				return nil
			},
			existingObjs: []client.Object{
				deploymentCopy(deployment1),
			},
			// Expect no update because there was an error
			expectedUpdated: false,
			// Expect err, because the UID in the request is older than the one on the server
			expectedError: fmt.Errorf(
				"failed to update object: apps/v1.Deployment(*v1.Deployment)[default/hello-world]: %w",
				errors.New("metadata.uid has changed: object may have been re-created")),
			expectedObj: func() client.Object {
				obj := deploymentCopy(deployment1)
				// no change persisted
				obj.SetUID("2")
				return obj
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// set the input object so tc.mutateFunc can update it
			inputObj = tc.obj
			// Reset once to allow one per test
			once = sync.Once{}

			scheme := core.Scheme
			fakeClient = fake.NewClient(t, scheme, tc.existingObjs...)
			ctx := context.Background()
			updated, err := Spec(ctx, fakeClient, tc.obj, tc.mutateFunc)
			testerrors.AssertEqual(t, tc.expectedError, err)
			assert.Equal(t, tc.expectedUpdated, updated)
			fakeClient.Check(t, tc.expectedObj)
		})
	}
}

func yamlToTypedObject(t *testing.T, yml string) client.Object {
	obj := &appsv1.Deployment{}
	_, _, err := decoder.Decode([]byte(yml), nil, obj)
	if err != nil {
		t.Fatalf("error decoding yaml: %v", err)
		return nil
	}
	return obj
}

func deploymentCopy(in client.Object) *appsv1.Deployment {
	return in.(*appsv1.Deployment).DeepCopy()
}
