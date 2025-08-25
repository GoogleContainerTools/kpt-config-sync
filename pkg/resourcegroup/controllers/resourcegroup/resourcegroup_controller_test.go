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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testcontroller"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"kpt.dev/configsync/pkg/testing/testwatch"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/common"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	rgName      = "group0"
	rgNamespace = metav1.NamespaceDefault
	inventoryID = rgNamespace + "_" + rgName
)

func TestReconcile(t *testing.T) {
	var channelKpt chan event.GenericEvent

	// Configure controller-manager to log to the test logger
	testLogger := testcontroller.NewTestLogger(t)
	controllerruntime.SetLogger(testLogger)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		// Disable metrics
		Metrics: metricsserver.Options{BindAddress: "0"},
		Logger:  testLogger.WithName("controller-manager"),
		// Use a client.WithWatch, instead of just a client.Client
		NewClient: func(cfg *rest.Config, opts client.Options) (client.Client, error) {
			return client.NewWithWatch(cfg, opts)
		},
	})
	require.NoError(t, err)
	// Get the watch client built by the manager
	c := mgr.GetClient().(client.WithWatch)

	ctx := t.Context()

	// Setup the controllers
	logger := testLogger.WithName("controllers")
	channelKpt = make(chan event.GenericEvent)
	resolver, err := typeresolver.ForManager(mgr, logger.WithName("typeresolver"))
	require.NoError(t, err)
	resMap := resourcemap.NewResourceMap()
	err = NewRGController(mgr, channelKpt, logger.WithName("resourcegroup"), resolver, resMap, 0)
	require.NoError(t, err)

	// Start the manager
	stopTestManager := testcontroller.StartTestManager(t, mgr)
	// Block test cleanup until manager is fully stopped
	defer stopTestManager()

	resources := []v1alpha1.ObjMetadata{}

	// Create a ResourceGroup object which does not include any resources
	rgKey := client.ObjectKey{
		Name:      rgName,
		Namespace: rgNamespace,
	}
	resgroupKpt := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rgName,
			Namespace: rgNamespace,
			Labels: map[string]string{
				common.InventoryLabel: inventoryID,
			},
		},
		Spec: v1alpha1.ResourceGroupSpec{
			Resources: resources,
		},
	}
	expectedStatus := v1alpha1.ResourceGroupStatus{
		ObservedGeneration: 0,
	}

	// Create the ResourceGroup spec (simulating InventoryResourceGroup.Apply)
	err = c.Create(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 1, 0, expectedStatus)

	// Update the ResourceGroup status (simulating InventoryResourceGroup.Apply)
	resgroupKpt.Status.ObservedGeneration = resgroupKpt.Generation
	err = c.Status().Update(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	expectedStatus.ObservedGeneration = 1
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 1, 0, expectedStatus)

	// Push an event to the channel, which will cause trigger a reconciliation for resgroup
	t.Log("Sending event to controller")
	channelKpt <- event.GenericEvent{Object: resgroupKpt}

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	expectedStatus.ObservedGeneration = 1
	expectedStatus.Conditions = []v1alpha1.Condition{
		newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
	}
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 1, 0, expectedStatus)
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
		Namespace: rgNamespace,
		GroupKind: v1alpha1.GroupKind{
			Group: "",
			Kind:  "Pod",
		},
	}
	resources = []v1alpha1.ObjMetadata{res1, res2}
	resgroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}

	// Update the ResourceGroup spec (simulating InventoryResourceGroup.Apply)
	err = c.Update(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 2, 2, expectedStatus)

	// Update the ResourceGroup status (simulating InventoryResourceGroup.Apply)
	resgroupKpt.Status.ObservedGeneration = resgroupKpt.Generation
	err = c.Status().Update(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	expectedStatus.ObservedGeneration = 2
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 2, 2, expectedStatus)

	t.Log("Sending event to controller")
	channelKpt <- event.GenericEvent{Object: resgroupKpt}

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: res1,
			Status:      v1alpha1.NotFound,
		},
		{
			ObjMetadata: res2,
			Status:      v1alpha1.NotFound,
		},
	}
	expectedStatus.ObservedGeneration = 2
	expectedStatus.Conditions = []v1alpha1.Condition{
		newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
	}
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 2, 2, expectedStatus)

	// Create res2
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      res2.Name,
			Namespace: res2.Namespace,
			Annotations: map[string]string{
				metadata.OwningInventoryKey: "other",
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

	err = c.Create(ctx, pod2, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	updatedPod := &corev1.Pod{}
	err = c.Get(ctx, types.NamespacedName{Name: res2.Name, Namespace: res2.Namespace}, updatedPod)
	require.NoError(t, err)
	require.Equal(t, corev1.PodPending, updatedPod.Status.Phase)

	// Create res1
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: res1.Name,
			Annotations: map[string]string{
				metadata.OwningInventoryKey: inventoryID,
			},
		},
	}
	err = c.Create(ctx, ns1, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	updatedNS := &corev1.Namespace{}
	err = c.Get(ctx, types.NamespacedName{Name: res1.Name, Namespace: ""}, updatedNS)
	require.NoError(t, err)
	require.Equal(t, corev1.NamespaceActive, updatedNS.Status.Phase)

	t.Log("Sending event to controller")
	channelKpt <- event.GenericEvent{Object: resgroupKpt}

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: res1,
			Status:      v1alpha1.Current,
		},
		{
			ObjMetadata: res2,
			Status:      v1alpha1.InProgress,
			Conditions: []v1alpha1.Condition{
				{
					Type:   v1alpha1.Ownership,
					Status: v1alpha1.TrueConditionStatus,
					Reason: v1alpha1.OwnershipUnmatch,
					Message: "This resource is owned by another ResourceGroup other. " +
						"The status only reflects the specification for the current object in ResourceGroup other.",
				},
			},
		},
	}
	expectedStatus.Conditions = []v1alpha1.Condition{
		newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
	}
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 2, 2, expectedStatus)

	// Set the resources to be {res1}
	resources = []v1alpha1.ObjMetadata{res1}
	require.NoError(t, err)
	resgroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}

	// Update the ResourceGroup spec (simulating InventoryResourceGroup.Apply)
	err = c.Update(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 3, 1, expectedStatus)

	// Update the ResourceGroup status (simulating InventoryResourceGroup.Apply)
	resgroupKpt.Status.ObservedGeneration = resgroupKpt.Generation
	err = c.Status().Update(ctx, resgroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)
	expectedStatus.ObservedGeneration = 3
	resgroupKpt = waitForResourceGroupStatus(t, ctx, c, rgKey, 3, 1, expectedStatus)

	t.Log("Sending event to controller")
	channelKpt <- event.GenericEvent{Object: resgroupKpt}

	// Verify that the reconciliation modifies the ResourceGroupStatus field correctly
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: res1,
			Status:      v1alpha1.Current,
		},
	}
	expectedStatus.ObservedGeneration = 3
	expectedStatus.Conditions = []v1alpha1.Condition{
		newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
		newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
	}
	_ = waitForResourceGroupStatus(t, ctx, c, rgKey, 3, 1, expectedStatus)
}

//nolint:revive // testing.T before context.Context
func waitForResourceGroupStatus(t *testing.T, ctx context.Context, c client.WithWatch, key client.ObjectKey, expectedGeneration, expectedResourceCount int, expectedStatus v1alpha1.ResourceGroupStatus) *v1alpha1.ResourceGroup {
	watcher, err := testwatch.WatchObject(ctx, c, &v1alpha1.ResourceGroupList{})
	require.NoError(t, err)
	// Cache the last known ResourceGroup
	var rgObj *v1alpha1.ResourceGroup
	condition := func(e watch.Event) error {
		rgObj = e.Object.(*v1alpha1.ResourceGroup)
		return validateResourceGroup(rgObj, expectedGeneration, expectedResourceCount, expectedStatus)
	}
	ctx, cancel := context.WithTimeoutCause(ctx, 10*time.Second, fmt.Errorf("timed out (10s)"))
	defer cancel()
	err = testwatch.WatchObjectUntil(ctx, c.Scheme(), watcher, key, condition)
	require.NoError(t, err)
	return rgObj
}

func validateResourceGroup(obj runtime.Object, expectedGeneration, expectedResourceCount int, expectedStatus v1alpha1.ResourceGroupStatus) error {
	rg := obj.(*v1alpha1.ResourceGroup)
	rgStatus := rg.Status

	// Ignore timestamps, since we can't fake them using the controller-runtime TestEnvironment
	opts := []cmp.Option{
		cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime"),
	}

	var err error
	if rg.Generation != int64(expectedGeneration) {
		err = errors.Join(err, fmt.Errorf("expected `metadata.generation` to equal %v, but got %v",
			expectedGeneration, rg.Generation))
	}
	if len(rg.Spec.Resources) != expectedResourceCount {
		err = errors.Join(err, fmt.Errorf("expected `len(spec.resources)` to equal %v, but got %v",
			expectedResourceCount, len(rg.Spec.Resources)))
	}
	if rgStatus.ObservedGeneration != expectedStatus.ObservedGeneration {
		err = errors.Join(err, fmt.Errorf("expected `status.observedGeneration` to equal %v, but got %v",
			expectedStatus.ObservedGeneration, rgStatus.ObservedGeneration))
	}
	if len(rgStatus.ResourceStatuses) != len(expectedStatus.ResourceStatuses) {
		err = errors.Join(err, fmt.Errorf("expected `len(status.resourceStatuses)` to equal %v, but got %v",
			expectedStatus.ObservedGeneration, rgStatus.ObservedGeneration))
	}
	if !cmp.Equal(expectedStatus.ResourceStatuses, rgStatus.ResourceStatuses, opts...) {
		err = errors.Join(err, fmt.Errorf("expected `status.resourceStatuses` to equal:\n%sbut got:\n%s\n%s",
			log.AsYAML(expectedStatus.ResourceStatuses),
			log.AsYAML(rgStatus.ResourceStatuses),
			cmp.Diff(expectedStatus.ResourceStatuses, rgStatus.ResourceStatuses)))
	}
	if len(rgStatus.Conditions) != len(expectedStatus.Conditions) {
		err = errors.Join(err, fmt.Errorf("expected `len(status.conditions)` to equal %v, but got %v",
			expectedStatus.Conditions, rgStatus.Conditions))
	}
	if !cmp.Equal(expectedStatus.Conditions, rgStatus.Conditions, opts...) {
		err = errors.Join(err, fmt.Errorf("expected `status.conditions` to equal:\n%sbut got:\n%s\n%s",
			log.AsYAML(expectedStatus.Conditions),
			log.AsYAML(rgStatus.Conditions),
			cmp.Diff(expectedStatus.Conditions, rgStatus.Conditions, opts...)))
	}
	if err == nil {
		klog.V(3).Info("Watch condition met")
	}
	return err
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

// TestUpdateReconcileStatusToReflectKstatus tests the
// UpdateReconcileStatusToReflectKstatus function.
func TestUpdateReconcileStatusToReflectKstatus(t *testing.T) {
	// Define test cases using a table-driven approach
	testCases := []struct {
		name          string
		status        v1alpha1.ResourceStatus
		expected      v1alpha1.Reconcile
		expectedError error
	}{
		// Apply Strategy tests
		{
			name: "Apply_Succeeded_Current",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Current,
			},
			expected: v1alpha1.ReconcileSucceeded,
		},
		{
			name: "Apply_Succeeded_InProgress",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.InProgress,
			},
			expected: v1alpha1.ReconcilePending,
		},
		{
			name: "Apply_Succeeded_Failed",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Failed,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Apply_Succeeded_Terminating",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Terminating,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Apply_Succeeded_NotFound",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.NotFound,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Apply_Succeeded_Unknown",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Unknown,
				Reconcile: v1alpha1.ReconcilePending, // Simulate previous reconcile status
			},
			expected: v1alpha1.ReconcilePending,
		},
		{
			name: "Apply_Succeeded_InvalidKstatus",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    "Invalid", // Invalid Kstatus
			},
			expected:      "",
			expectedError: fmt.Errorf("invalid kstatus: %s", "Invalid"),
		},
		{
			name: "Apply_Pending",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationPending,
			},
			expected: v1alpha1.ReconcilePending,
		},
		{
			name: "Apply_Skipped",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSkipped,
			},
			expected: v1alpha1.ReconcileSkipped,
		},
		{
			name: "Apply_Failed",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationFailed,
			},
			expected: v1alpha1.ReconcileSkipped,
		},
		{
			name: "Apply_InvalidActuation",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Apply,
				Actuation: "Invalid", // Invalid Actuation
			},
			expected:      "",
			expectedError: fmt.Errorf("invalid actuation status: %s", "Invalid"),
		},

		// Delete Strategy tests
		{
			name: "Delete_Succeeded_Current",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Current,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Delete_Succeeded_InProgress",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.InProgress,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Delete_Succeeded_Failed",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Failed,
			},
			expected: v1alpha1.ReconcileFailed,
		},
		{
			name: "Delete_Succeeded_Terminating",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.Terminating,
			},
			expected: v1alpha1.ReconcilePending,
		},
		{
			name: "Delete_Succeeded_NotFound",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    v1alpha1.NotFound,
			},
			expected: v1alpha1.ReconcileSucceeded,
		},
		{
			name: "Delete_Succeeded_InvalidKstatus",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Status:    "Invalid", // Invalid Kstatus
			},
			expected:      "",
			expectedError: fmt.Errorf("invalid kstatus: %s", "Invalid"),
		},
		{
			name: "Delete_Pending",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationPending,
			},
			expected: v1alpha1.ReconcilePending,
		},
		{
			name: "Delete_Skipped",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSkipped,
			},
			expected: v1alpha1.ReconcileSkipped,
		},
		{
			name: "Delete_Failed",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationFailed,
			},
			expected: v1alpha1.ReconcileSkipped,
		},
		{
			name: "Delete_InvalidActuation",
			status: v1alpha1.ResourceStatus{
				Strategy:  v1alpha1.Delete,
				Actuation: "Invalid", // Invalid Actuation
			},
			expected:      "",
			expectedError: fmt.Errorf("invalid actuation status: %s", "Invalid"),
		},

		// Invalid Strategy tests
		{
			name: "InvalidStrategy",
			status: v1alpha1.ResourceStatus{
				Strategy: "Invalid", // Invalid Strategy
			},
			expected:      "",
			expectedError: fmt.Errorf("invalid actuation strategy: %s", "Invalid"),
		},
	}

	// Iterate through the test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Call the function under test
			actual, err := UpdateReconcileStatusToReflectKstatus(tc.status)
			assert.Equal(t, tc.expected, actual)
			testerrors.AssertEqual(t, tc.expectedError, err)
		})
	}
}

func TestUpdateStatusToReflectActuation(t *testing.T) {
	tests := []struct {
		name      string
		resStatus v1alpha1.ResourceStatus
		want      v1alpha1.Status
	}{
		{
			name: "Status should equal current status when actuation is status is successful",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.Current,
				Actuation: v1alpha1.ActuationSucceeded,
			},
			want: v1alpha1.Current,
		},
		{
			name: "Return status field when actuation is status is empty",
			resStatus: v1alpha1.ResourceStatus{
				Status: v1alpha1.InProgress,
			},
			want: v1alpha1.InProgress,
		},
		{
			name: "Return unknown when actuation is not successful",
			resStatus: v1alpha1.ResourceStatus{
				Actuation: v1alpha1.ActuationPending,
			},
			want: v1alpha1.Unknown,
		},
		{
			name: "Return not found when status is not found already",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Actuation: v1alpha1.ActuationPending,
			},
			want: v1alpha1.NotFound,
		},
		{
			name: "Return not found when status is not found already - disregard actuation success",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Actuation: v1alpha1.ActuationSucceeded,
			},
			want: v1alpha1.NotFound,
		},
		{
			name: "Return Terminating when Terminating even if apply and reconcile previously succeeded",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.Terminating,
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			want: v1alpha1.Terminating,
		},
		{
			name: "Return Terminating when Terminating even if delete and reconcile previously succeeded",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.Terminating,
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			want: v1alpha1.Terminating,
		},
		{
			name: "Return NotFound when NotFound even if apply and reconcile previously succeeded",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Strategy:  v1alpha1.Apply,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			want: v1alpha1.NotFound,
		},
		{
			name: "Return NotFound when NotFound when strategy is delete",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.NotFound,
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			want: v1alpha1.NotFound,
		},
		{
			name: "Return Terminating if status is Terminating and strategy is delete, even if reconcile previously succeeded",
			resStatus: v1alpha1.ResourceStatus{
				Status:    v1alpha1.Terminating,
				Strategy:  v1alpha1.Delete,
				Actuation: v1alpha1.ActuationSucceeded,
				Reconcile: v1alpha1.ReconcileSucceeded,
			},
			want: v1alpha1.Terminating,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UpdateStatusToReflectActuation(tc.resStatus); got != tc.want {
				t.Errorf("ActuationStatusToLegacy() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestComputeStatus(t *testing.T) {
	testLogger := testcontroller.NewTestLogger(t)
	controllerruntime.SetLogger(testLogger)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	podRunning := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-abc",
			Namespace: "default",
			Annotations: map[string]string{
				metadata.OwningInventoryKey: inventoryID,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	podSucceeded := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-abc",
			Namespace: "default",
			Annotations: map[string]string{
				metadata.OwningInventoryKey: inventoryID,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}

	objMetaPodABC := v1alpha1.ObjMetadata{
		Name:      "pod-abc",
		Namespace: "default",
		GroupKind: v1alpha1.GroupKind{Kind: "Pod", Group: ""},
	}
	objMetaPodXYZ := v1alpha1.ObjMetadata{
		Name:      "pod-xyz",
		Namespace: "default",
		GroupKind: v1alpha1.GroupKind{Kind: "Pod", Group: ""},
	}

	testCases := []struct {
		name               string
		initialObjects     []client.Object
		objMetas           []v1alpha1.ObjMetadata
		cachedStatuses     map[v1alpha1.ObjMetadata]*resourcemap.CachedStatus
		discoveryResources []*metav1.APIResourceList
		expectedStatuses   []v1alpha1.ResourceStatus
	}{
		{
			name:           "cached status is NotFound, should skip client GET and return NotFound",
			initialObjects: nil,
			objMetas:       []v1alpha1.ObjMetadata{objMetaPodABC},
			cachedStatuses: map[v1alpha1.ObjMetadata]*resourcemap.CachedStatus{
				objMetaPodABC: {Status: v1alpha1.NotFound},
			},
			expectedStatuses: []v1alpha1.ResourceStatus{
				{
					ObjMetadata: objMetaPodABC,
					Status:      v1alpha1.NotFound,
				},
			},
		},
		{
			name:           "cached status is Current, should use cache and return Current",
			initialObjects: []client.Object{podRunning},
			objMetas:       []v1alpha1.ObjMetadata{objMetaPodABC},
			cachedStatuses: map[v1alpha1.ObjMetadata]*resourcemap.CachedStatus{
				objMetaPodABC: {Status: v1alpha1.Current, InventoryID: inventoryID},
			},
			expectedStatuses: []v1alpha1.ResourceStatus{
				{
					ObjMetadata: objMetaPodABC,
					Status:      v1alpha1.Current,
					Conditions:  []v1alpha1.Condition{},
				},
			},
		},
		{
			name:           "not cached and object not found, should GET from client and return NotFound",
			initialObjects: nil,
			objMetas:       []v1alpha1.ObjMetadata{objMetaPodXYZ},
			cachedStatuses: nil,
			expectedStatuses: []v1alpha1.ResourceStatus{
				{
					ObjMetadata: objMetaPodXYZ,
					Status:      v1alpha1.NotFound,
				},
			},
		},
		{
			name:           "not cached and object exists, should GET from client and return Current",
			initialObjects: []client.Object{podSucceeded},
			objMetas:       []v1alpha1.ObjMetadata{objMetaPodABC},
			cachedStatuses: nil,
			discoveryResources: []*metav1.APIResourceList{
				{
					GroupVersion: "v1",
					APIResources: []metav1.APIResource{
						{Name: "pods", SingularName: "pod", Namespaced: true, Kind: "Pod"},
					},
				},
			},
			expectedStatuses: []v1alpha1.ResourceStatus{
				{
					ObjMetadata: objMetaPodABC,
					Status:      v1alpha1.Current,
					Conditions:  []v1alpha1.Condition{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClient(t, scheme, tc.initialObjects...)
			resMap := resourcemap.NewResourceMap()
			for objMeta, cachedStatus := range tc.cachedStatuses {
				resMap.SetStatus(objMeta, cachedStatus)
			}

			discoveryClient := &discoveryfake.FakeDiscovery{Fake: &clientgotesting.Fake{}}
			discoveryClient.Resources = tc.discoveryResources
			resolver := typeresolver.NewTypeResolver(discoveryClient, testLogger)
			if tc.discoveryResources != nil {
				require.NoError(t, resolver.Refresh(t.Context()))
			}

			r := &reconciler{
				LoggingController: controllers.NewLoggingController(testLogger),
				client:            fakeClient,
				resolver:          resolver,
				resMap:            resMap,
			}

			resultStatuses := r.computeStatus(t.Context(), inventoryID, v1alpha1.ResourceGroupStatus{}, tc.objMetas, types.NamespacedName{}, true)

			opts := []cmp.Option{
				cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime"),
			}
			if diff := cmp.Diff(tc.expectedStatuses, resultStatuses, opts...); diff != "" {
				t.Errorf("computeStatus returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
