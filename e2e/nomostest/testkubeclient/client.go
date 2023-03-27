// Copyright 2023 Google LLC
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

package testkubeclient

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testlogger"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeClient wraps client.KubeClient to handle logging, default test-scoped context,
// and type checking in the schema before API calls.
//
// Includes some advanced helper functions, like Apply, MergePatch, and
// GetDeploymentPod.
type KubeClient struct {
	// Context to use, if not specified by the method.
	Context context.Context
	// Client to use to manage Kubernetes objects.
	Client client.Client
	// Logger for methods to use.
	Logger *testlogger.TestLogger
}

func fmtObj(obj client.Object) string {
	return fmt.Sprintf("%s/%s %T", obj.GetNamespace(), obj.GetName(), obj)
}

// ObjectTypeMustExist returns an error if the passed type is not declared in
// the client's scheme.
func (tc *KubeClient) ObjectTypeMustExist(o client.Object) error {
	gvks, _, _ := tc.Client.Scheme().ObjectKinds(o)
	if len(gvks) == 0 {
		return errors.Errorf("unknown type %T %v. Add it to nomostest.newScheme().", o, o.GetObjectKind().GroupVersionKind())
	}
	return nil
}

// Get is identical to Get defined for clietc.Client, except:
//
// 1) Context implicitly uses the one created for the test case.
// 2) name and namespace are strings instead of requiring client.ObjectKey.
//
// Leave namespace as empty string for cluster-scoped resources.
func (tc *KubeClient) Get(name, namespace string, obj client.Object) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	if obj.GetResourceVersion() != "" {
		// If obj is already populated, this can cause the final obj to be a
		// composite of multiple states of the object on the cluster.
		//
		// If this is due to a retry loop, remember to create a new instance to
		// populate for each loop.
		return errors.Errorf("called .Get on already-populated object %v: %v", obj.GetObjectKind().GroupVersionKind(), obj)
	}
	return tc.Client.Get(tc.Context, client.ObjectKey{Name: name, Namespace: namespace}, obj)
}

// List is identical to List defined for clietc.Client, but without requiring Context.
func (tc *KubeClient) List(obj client.ObjectList, opts ...client.ListOption) error {
	return tc.Client.List(tc.Context, obj, opts...)
}

// Create is identical to Create defined for clietc.Client, but without requiring Context.
func (tc *KubeClient) Create(obj client.Object, opts ...client.CreateOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("creating %s", fmtObj(obj))
	AddTestLabel(obj)
	opts = append(opts, client.FieldOwner(FieldManager))
	return tc.Client.Create(tc.Context, obj, opts...)
}

// Update is identical to Update defined for clietc.Client, but without requiring Context.
// All fields will be adopted by the nomostest field manager.
func (tc *KubeClient) Update(obj client.Object, opts ...client.UpdateOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("updating %s", fmtObj(obj))
	opts = append(opts, client.FieldOwner(FieldManager))
	return tc.Client.Update(tc.Context, obj, opts...)
}

// Apply wraps Patch to perform a server-side apply.
// All non-nil fields will be adopted by the nomostest field manager.
func (tc *KubeClient) Apply(obj client.Object, opts ...client.PatchOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("applying %s", fmtObj(obj))
	AddTestLabel(obj)
	opts = append(opts, client.FieldOwner(FieldManager), client.ForceOwnership)
	return tc.Client.Patch(tc.Context, obj, client.Apply, opts...)
}

// Delete is identical to Delete defined for clietc.Client, but without requiring Context.
func (tc *KubeClient) Delete(obj client.Object, opts ...client.DeleteOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("deleting %s", fmtObj(obj))
	return tc.Client.Delete(tc.Context, obj, opts...)
}

// DeleteAllOf is identical to DeleteAllOf defined for clietc.Client, but without requiring Context.
func (tc *KubeClient) DeleteAllOf(obj client.Object, opts ...client.DeleteAllOfOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("deleting all of %T", obj)
	return tc.Client.DeleteAllOf(tc.Context, obj, opts...)
}

// MergePatch uses the object to construct a merge patch for the fields provided.
// All specified fields will be adopted by the nomostest field manager.
func (tc *KubeClient) MergePatch(obj client.Object, patch string, opts ...client.PatchOption) error {
	if err := tc.ObjectTypeMustExist(obj); err != nil {
		return err
	}
	tc.Logger.Debugf("Applying patch %s", patch)
	AddTestLabel(obj)
	opts = append(opts, client.FieldOwner(FieldManager))
	return tc.Client.Patch(tc.Context, obj, client.RawPatch(types.MergePatchType, []byte(patch)), opts...)
}

// GetDeploymentPod is a convenience method to look up the pod for a deployment.
// It requires that exactly one pod exists, is running, and that the deployment
// uses label selectors to uniquely identify its pods.
// This is primarily useful for finding the current pod for a reconciler or
// other single-replica controller deployments.
func (tc *KubeClient) GetDeploymentPod(deploymentName, namespace string, retrytTimeout time.Duration) (*corev1.Pod, error) {
	deploymentNN := types.NamespacedName{Name: deploymentName, Namespace: namespace}
	var pod *corev1.Pod
	took, err := retry.Retry(retrytTimeout, func() error {
		deployment := &appsv1.Deployment{}
		if err := tc.Get(deploymentNN.Name, deploymentNN.Namespace, deployment); err != nil {
			return err
		}
		// Note: Waiting for updated & ready should be good enough.
		// But if there's problems with flapping pods, we should wait for
		// AvailableReplicas too, which respects MinReadySeconds.
		// We're choosing to use ReadyReplicas here instead because it's faster.
		if deployment.Status.UpdatedReplicas != 1 {
			return errors.Errorf("deployment has %d updated pods, expected 1: %s",
				deployment.Status.UpdatedReplicas, deploymentNN)
		}
		if deployment.Status.ReadyReplicas != 1 {
			return errors.Errorf("deployment has %d ready pods, expected 1: %s",
				deployment.Status.ReadyReplicas, deploymentNN)
		}
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			return err
		}
		if selector.Empty() {
			return errors.Errorf("deployment has no label selectors: %s", deploymentNN)
		}
		pods := &corev1.PodList{}
		err = tc.List(pods, client.InNamespace(deploymentNN.Namespace),
			client.MatchingLabelsSelector{Selector: selector})
		if err != nil {
			return err
		}
		if len(pods.Items) != 1 {
			return errors.Errorf("deployment has %d pods, expected 1: %s",
				len(pods.Items), deploymentNN)
		}
		pod = pods.Items[0].DeepCopy()
		if pod.Status.Phase != corev1.PodRunning {
			return errors.Errorf("pod has status %q, want %q: %s",
				pod.Status.Phase, corev1.PodRunning, client.ObjectKeyFromObject(pod))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	tc.Logger.Infof("took %v to wait for deployment pod", took)
	tc.Logger.Debugf("Found deployment pod: %s", client.ObjectKeyFromObject(pod))
	return pod, nil
}
