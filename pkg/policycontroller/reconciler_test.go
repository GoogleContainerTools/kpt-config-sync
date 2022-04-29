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

package policycontroller

import (
	"context"
	"reflect"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/policycontroller/constraint"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestConstraintGVKs(t *testing.T) {
	cm := &clientMock{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	thr := &throttler{make(chan map[schema.GroupVersionKind]bool)}
	go thr.start(ctx, &restartableManagerStub{})

	cr := &crdReconciler{
		client:          cm,
		thr:             thr,
		crdKinds:        map[string]schema.GroupVersionKind{},
		constraintKinds: map[schema.GroupVersionKind]bool{},
	}

	// Verify the initial empty case
	gvks := cr.establishedConstraints()
	if len(gvks) != 0 {
		t.Errorf("want empty GVK map; got %v", gvks)
	}

	// Create a FooConstraint that is not yet established.
	cm.nextGet = constraintCRD("FooConstraint", false)
	_, err := cr.Reconcile(ctx, request("foo"))
	if err != nil {
		t.Fatal(err)
	}
	gvks = cr.establishedConstraints()
	if len(gvks) != 0 {
		t.Errorf("want empty GVK map; got %v", gvks)
	}

	// Create a random CRD that is established (but should be ignored).
	cm.nextGet = randomCRD("Anvil", true)
	_, err = cr.Reconcile(ctx, request("anvil"))
	if err != nil {
		t.Fatal(err)
	}
	gvks = cr.establishedConstraints()
	if len(gvks) != 0 {
		t.Errorf("want empty GVK map; got %v", gvks)
	}

	// Create a BarConstraint that is established.
	cm.nextGet = constraintCRD("BarConstraint", true)
	_, err = cr.Reconcile(ctx, request("bar"))
	if err != nil {
		t.Fatal(err)
	}
	gvks = cr.establishedConstraints()
	if len(gvks) != 1 || !gvks[constraint.GVK("BarConstraint")] {
		t.Errorf("want BarConstraint; got %v", gvks)
	}

	// Update FooConstraint to be established along with BarConstraint.
	cm.nextGet = constraintCRD("FooConstraint", true)
	_, err = cr.Reconcile(ctx, request("foo"))
	if err != nil {
		t.Fatal(err)
	}
	gvks = cr.establishedConstraints()
	if len(gvks) != 2 || !gvks[constraint.GVK("FooConstraint")] || !gvks[constraint.GVK("BarConstraint")] {
		t.Errorf("want FooConstraint, BarConstraint; got %v", gvks)
	}

	// Delete BarConstraint from the cluster.
	cm.nextErr = errors.NewNotFound(schema.GroupResource{}, "bar")
	_, err = cr.Reconcile(ctx, request("bar"))
	if err != nil {
		t.Fatal(err)
	}
	gvks = cr.establishedConstraints()
	if len(gvks) != 1 || !gvks[constraint.GVK("FooConstraint")] {
		t.Errorf("want FooConstraint; got %v", gvks)
	}
}

func request(name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name},
	}
}

func constraintCRD(kind string, isEstablished bool) *apiextensionsv1.CustomResourceDefinition {
	crd := randomCRD(kind, isEstablished)
	crd.Spec.Group = constraint.GVK(kind).Group
	return crd
}

func randomCRD(kind string, isEstablished bool) *apiextensionsv1.CustomResourceDefinition {
	established := apiextensionsv1.ConditionTrue
	if !isEstablished {
		established = apiextensionsv1.ConditionFalse
	}

	return &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "somethingsomething",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: kind,
			},
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
				{
					Type:   apiextensionsv1.Established,
					Status: established,
				},
			},
		},
	}
}

type clientMock struct {
	client.Client

	nextGet client.Object
	nextErr error
}

func (c *clientMock) Get(_ context.Context, _ client.ObjectKey, obj client.Object) error {
	if c.nextErr != nil {
		err := c.nextErr
		c.nextErr = nil
		return err
	}

	outVal := reflect.ValueOf(obj)
	reflect.Indirect(outVal).Set(reflect.Indirect(reflect.ValueOf(c.nextGet)))
	c.nextGet = nil
	return nil
}

type restartableManagerStub struct{}

var _ watch.RestartableManager = &restartableManagerStub{}

func (r restartableManagerStub) Restart(_ map[schema.GroupVersionKind]bool, _ bool) (bool, error) {
	return false, nil
}
