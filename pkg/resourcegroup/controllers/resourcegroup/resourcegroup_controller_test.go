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

package resourcegroup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const contextResourceGroupControllerKey = contextKey("resourcegroup-controller")

var c client.Client
var ctx context.Context

func TestReconcile(t *testing.T) {
	var channelKpt chan event.GenericEvent
	var namespace = metav1.NamespaceDefault

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	assert.NoError(t, err)
	c = mgr.GetClient()

	klog.InitFlags(nil)
	logger := klogr.New().WithName("controllers").WithName(v1alpha1.ResourceGroupKind)

	ctx = context.WithValue(context.TODO(), contextResourceGroupControllerKey, logger)

	// Setup the controller
	channelKpt = make(chan event.GenericEvent)
	resolver, err := typeresolver.NewTypeResolver(mgr, logger)
	assert.NoError(t, err)
	resMap := resourcemap.NewResourceMap()
	err = NewRGController(mgr, channelKpt, logger, resolver, resMap, 0)
	assert.NoError(t, err)

	// Start the manager
	StartTestManager(t, mgr)
	time.Sleep(10 * time.Second)

	resources := []v1alpha1.ObjMetadata{}

	// Create a ResourceGroup object which does not include any resources
	resgroupName := "group0"
	resgroupNamespacedName := types.NamespacedName{
		Name:      resgroupName,
		Namespace: namespace,
	}
	resgroupKpt := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resgroupName,
			Namespace: namespace,
			Labels: map[string]string{
				common.InventoryLabel: "group0",
			},
		},
		Spec: v1alpha1.ResourceGroupSpec{
			Resources: resources,
		},
	}
	err = c.Create(ctx, resgroupKpt)
	assert.NoError(t, err)
	// Wait 5 seconds before querying resgroup from API server
	time.Sleep(5 * time.Second)

	// Verify the ResourceGroup was created successfully
	updatedResgroupKpt := &v1alpha1.ResourceGroup{}
	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)
	verifyClusterResourceGroup(t, updatedResgroupKpt, 1, 0, v1alpha1.ResourceGroupStatus{})

	// Push an event to the channel, which will cause trigger a reconciliation for resgroup
	channelKpt <- event.GenericEvent{Object: resgroupKpt}
	time.Sleep(5 * time.Second)

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)
	expectedStatus := v1alpha1.ResourceGroupStatus{
		ObservedGeneration: 1,
		Conditions: []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
			newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		},
	}
	verifyClusterResourceGroup(t, updatedResgroupKpt, 1, 0, expectedStatus)
	// Add two non-existing resources
	res1 := v1alpha1.ObjMetadata{
		Name:      "ns1",
		Namespace: "",
		GroupKind: v1alpha1.GroupKind{
			Group: "",
			Kind:  "Namespace",
		},
	}
	res2 := v1alpha1.ObjMetadata{
		Name:      "pod1",
		Namespace: namespace,
		GroupKind: v1alpha1.GroupKind{
			Group: "",
			Kind:  "Pod",
		},
	}
	resources = []v1alpha1.ObjMetadata{res1, res2}
	updatedResgroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}

	err = c.Update(ctx, updatedResgroupKpt)
	assert.NoError(t, err)
	time.Sleep(5 * time.Second)

	channelKpt <- event.GenericEvent{Object: resgroupKpt}
	time.Sleep(5 * time.Second)

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)
	expectedStatus = v1alpha1.ResourceGroupStatus{
		ObservedGeneration: 2,
		ResourceStatuses: []v1alpha1.ResourceStatus{
			{
				ObjMetadata: res1,
				Status:      v1alpha1.NotFound,
			},
			{
				ObjMetadata: res2,
				Status:      v1alpha1.NotFound,
			},
		},
		Conditions: []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
			newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		},
	}
	verifyClusterResourceGroup(t, updatedResgroupKpt, 2, 2, expectedStatus)

	// Create res2
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      res2.Name,
			Namespace: res2.Namespace,
			Annotations: map[string]string{
				owningInventoryKey: "other",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "hello-world",
					Image: "hello-world",
				},
			},
		},
	}

	err = c.Create(ctx, pod2)
	assert.NoError(t, err)
	time.Sleep(5 * time.Second)

	updatedPod := &corev1.Pod{}
	err = c.Get(ctx, types.NamespacedName{Name: res2.Name, Namespace: res2.Namespace}, updatedPod)
	assert.NoError(t, err)
	assert.Equal(t, corev1.PodPending, updatedPod.Status.Phase)

	// Create res1
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: res1.Name,
			Annotations: map[string]string{
				owningInventoryKey: "group0",
			},
		},
	}
	err = c.Create(ctx, ns1)
	assert.NoError(t, err)
	time.Sleep(2 * time.Second)

	updatedNS := &corev1.Namespace{}
	err = c.Get(ctx, types.NamespacedName{Name: res1.Name, Namespace: ""}, updatedNS)
	assert.NoError(t, err)
	assert.Equal(t, corev1.NamespaceActive, updatedNS.Status.Phase)

	channelKpt <- event.GenericEvent{Object: resgroupKpt}
	time.Sleep(5 * time.Second)

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)
	expectedStatus = v1alpha1.ResourceGroupStatus{
		ObservedGeneration: 2,
		ResourceStatuses: []v1alpha1.ResourceStatus{
			{
				ObjMetadata: res1,
				Status:      v1alpha1.Current,
			},
			{
				ObjMetadata: res2,
				Status:      v1alpha1.InProgress,
				Conditions: []v1alpha1.Condition{
					{
						Type:    v1alpha1.Ownership,
						Status:  v1alpha1.TrueConditionStatus,
						Reason:  v1alpha1.OwnershipUnmatch,
						Message: "This object is owned by another inventory object with id other",
					},
				},
			},
		},
		Conditions: []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
			newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		},
	}
	verifyClusterResourceGroup(t, updatedResgroupKpt, 2, 2, expectedStatus)

	// Set the resources to be {res1}
	resources = []v1alpha1.ObjMetadata{res1}
	assert.NoError(t, err)
	updatedResgroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}
	err = c.Update(ctx, updatedResgroupKpt)
	assert.NoError(t, err)
	time.Sleep(5 * time.Second)

	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)

	verifyClusterResourceGroup(t, updatedResgroupKpt, 3, 1, expectedStatus)

	channelKpt <- event.GenericEvent{Object: resgroupKpt}
	time.Sleep(5 * time.Second)

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	err = c.Get(ctx, resgroupNamespacedName, updatedResgroupKpt)
	assert.NoError(t, err)
	expectedStatus = v1alpha1.ResourceGroupStatus{
		ObservedGeneration: 3,
		ResourceStatuses: []v1alpha1.ResourceStatus{
			{
				ObjMetadata: res1,
				Status:      v1alpha1.Current,
			},
		},
		Conditions: []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
			newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		},
	}
	verifyClusterResourceGroup(t, updatedResgroupKpt, 3, 1, expectedStatus)
}

func verifyClusterResourceGroup(t *testing.T, rg runtime.Object, gen int, num int, status v1alpha1.ResourceGroupStatus) {
	var generation int64
	var resourceNum int
	var actualStatus v1alpha1.ResourceGroupStatus
	rgConfigSync, ok := rg.(*v1alpha1.ResourceGroup)
	assert.True(t, ok)
	generation = rgConfigSync.Generation
	resourceNum = len(rgConfigSync.Spec.Resources)
	actualStatus = rgConfigSync.Status
	assert.Equal(t, int64(gen), generation)
	assert.Equal(t, num, resourceNum)
	assert.Equal(t, status.ObservedGeneration, actualStatus.ObservedGeneration)
	assert.Equal(t, len(status.ResourceStatuses), len(actualStatus.ResourceStatuses))
	for i, r := range actualStatus.ResourceStatuses {
		assert.Equal(t, status.ResourceStatuses[i].Status, r.Status)
	}
	assert.Equal(t, len(status.Conditions), len(actualStatus.Conditions))
	for i, c := range actualStatus.Conditions {
		assert.Equal(t, status.Conditions[i].Type, c.Type)
		assert.Equal(t, status.Conditions[i].Status, c.Status)
	}
}

func TestAggregateResourceStatuses(t *testing.T) {
	currentStatus := v1alpha1.ResourceStatus{
		Status: v1alpha1.Current,
	}
	inProgressStatus := v1alpha1.ResourceStatus{
		Status: v1alpha1.InProgress,
	}
	unknownStatus := v1alpha1.ResourceStatus{
		Status: v1alpha1.Unknown,
	}
	terminatingStatus := v1alpha1.ResourceStatus{
		Status: v1alpha1.Terminating,
	}
	failedStatus1 := v1alpha1.ResourceStatus{
		ObjMetadata: v1alpha1.ObjMetadata{
			Name:      "name1",
			Namespace: "ns1",
			GroupKind: v1alpha1.GroupKind{
				Group: "group1",
				Kind:  "kind1",
			},
		},
		Status: v1alpha1.Failed,
	}
	failedStatus2 := v1alpha1.ResourceStatus{
		ObjMetadata: v1alpha1.ObjMetadata{
			Name:      "name2",
			Namespace: "ns2",
			GroupKind: v1alpha1.GroupKind{
				Group: "group2",
				Kind:  "kind2",
			},
		},
		Status: v1alpha1.Failed,
	}
	tests := map[string]struct {
		input           []v1alpha1.ResourceStatus
		expectedType    v1alpha1.ConditionType
		expectedStatus  v1alpha1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		"should return a True Stalled condition with one failed component": {
			input:           []v1alpha1.ResourceStatus{currentStatus, failedStatus1},
			expectedType:    v1alpha1.Stalled,
			expectedStatus:  v1alpha1.TrueConditionStatus,
			expectedReason:  ComponentFailed,
			expectedMessage: componentFailedMsgPrefix + "group1/kind1/ns1/name1",
		},
		"should return a True Stalled condition with two failed components": {
			input:           []v1alpha1.ResourceStatus{currentStatus, failedStatus1, failedStatus2},
			expectedType:    v1alpha1.Stalled,
			expectedStatus:  v1alpha1.TrueConditionStatus,
			expectedReason:  ComponentFailed,
			expectedMessage: componentFailedMsgPrefix + "group1/kind1/ns1/name1, group2/kind2/ns2/name2",
		},
		"should return a False Stalled condition": {
			input: []v1alpha1.ResourceStatus{currentStatus,
				inProgressStatus, unknownStatus, terminatingStatus},
			expectedType:    v1alpha1.Stalled,
			expectedStatus:  v1alpha1.FalseConditionStatus,
			expectedReason:  FinishReconciling,
			expectedMessage: "finish reconciling",
		},
	}
	for name, tc := range tests {
		t.Run(fmt.Sprintf("aggregateResourceStatuses %s", name), func(t *testing.T) {
			cond := aggregateResourceStatuses(tc.input)
			assert.Equal(t, tc.expectedType, cond.Type)
			assert.Equal(t, tc.expectedStatus, cond.Status)
			assert.Equal(t, tc.expectedReason, cond.Reason)
			assert.Equal(t, tc.expectedMessage, cond.Message)
		})
	}
}

func TestReconcileTimeout(t *testing.T) {
	tests := map[string]struct {
		resourceCount int
		expected      time.Duration
	}{
		"should return 30 seconds when there is no resources": {
			resourceCount: 0,
			expected:      30 * time.Second,
		},
		"should return 60 seconds when there are 750 resources": {
			resourceCount: 0,
			expected:      30 * time.Second,
		},
		"should return 120 seconds when there are 2234 resources": {
			resourceCount: 0,
			expected:      30 * time.Second,
		},
		"should return 300 seconds when there are very large number of resources": {
			resourceCount: 0,
			expected:      30 * time.Second,
		},
	}
	for name, tc := range tests {
		t.Run(fmt.Sprintf("getReconcileTimeOut %s", name), func(t *testing.T) {
			actual := getReconcileTimeOut(tc.resourceCount)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestActuationStatusToLegacy(t *testing.T) {
	tests := []struct {
		name      string
		resStatus v1alpha1.ResourceStatus
		want      v1alpha1.Status
	}{
		{
			"Status should equal current status when actuation is status is successful",
			v1alpha1.ResourceStatus{
				Status:    v1alpha1.Current,
				Actuation: v1alpha1.ActuationSucceeded,
			},
			v1alpha1.Current,
		},
		{
			"Return status field when actuation is status is empty",
			v1alpha1.ResourceStatus{
				Status: v1alpha1.InProgress,
			},
			v1alpha1.InProgress,
		},
		{
			"Return unknown when actuation is not successful",
			v1alpha1.ResourceStatus{
				Actuation: v1alpha1.ActuationPending,
			},
			v1alpha1.Unknown,
		},
		{
			"Return not found when status is not found already",
			v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Actuation: v1alpha1.ActuationPending,
			},
			v1alpha1.NotFound,
		},
		{
			"Return not found when status is not found already - disregard actuation success",
			v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Actuation: v1alpha1.ActuationSucceeded,
			},
			v1alpha1.NotFound,
		},
		{
			"Return Current if both Actuation and Reconcile succeeded",
			v1alpha1.ResourceStatus{
				Status:    v1alpha1.Unknown,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			v1alpha1.Current,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ActuationStatusToLegacy(tc.resStatus); got != tc.want {
				t.Errorf("ActuationStatusToLegacy() = %v, want %v", got, tc.want)
			}
		})
	}
}
