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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/util"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// These are used as keys in calls to r.log.Info
	executedOperation    = "operation"
	operationSubjectName = "name"

	// gitSecretRefField is the path of the field in the RootSync|RepoSync CRDs
	// that we wish to use as the "object reference".
	// It will be used in both the indexing and watching.
	gitSecretRefField = ".spec.git.secretRef.name"

	// caCertSecretRefField is the path of the field in the RootSync|RepoSync CRDs
	// that we wish to use as the "object reference".
	// It will be used in both the indexing and watching.
	caCertSecretRefField = ".spec.git.caCertSecretRef.name"

	// helmSecretRefField is the path of the field in the RootSync|RepoSync CRDs
	// that we wish to use as the "object reference".
	// It will be used in both the indexing and watching.
	helmSecretRefField = ".spec.helm.secretRef.name"

	// fleetMembershipName is the name of the fleet membership
	fleetMembershipName = "membership"

	logFieldKind   = "kind"
	logFieldObject = "object"
)

// reconcilerBase provides common data and methods for the RepoSync and RootSync reconcilers
type reconcilerBase struct {
	clusterName             string
	client                  client.Client
	log                     logr.Logger
	scheme                  *runtime.Scheme
	isAutopilotCluster      *bool
	reconcilerPollingPeriod time.Duration
	hydrationPollingPeriod  time.Duration
	membership              *hubv1.Membership

	// syncKind is the kind of the sync object: RootSync or RepoSync.
	syncKind string
}

func (r *reconcilerBase) upsertServiceAccount(
	ctx context.Context,
	reconcilerRef types.NamespacedName,
	auth configsync.AuthType,
	email string,
	labelMap map[string]string,
	refs ...metav1.OwnerReference,
) (client.ObjectKey, error) {
	childSARef := reconcilerRef
	childSA := &corev1.ServiceAccount{}
	childSA.Name = childSARef.Name
	childSA.Namespace = childSARef.Namespace
	r.addLabels(childSA, labelMap)

	op, err := controllerruntime.CreateOrUpdate(ctx, r.client, childSA, func() error {
		// Update ownerRefs for RootSync ServiceAccount.
		// Do not set ownerRefs for RepoSync ServiceAccount, since Reconciler Manager,
		// performs garbage collection for Reposync controller resources.
		if len(refs) > 0 {
			childSA.OwnerReferences = refs
		}
		// Update annotation when Workload Identity is enabled on a GKE cluster.
		// In case, Workload Identity is not enabled on a cluster and spec.git.auth: gcpserviceaccount,
		// the added annotation will be a no-op.
		if auth == configsync.AuthGCPServiceAccount {
			core.SetAnnotation(childSA, GCPSAAnnotationKey, email)
		}
		return nil
	})
	if err != nil {
		return childSARef, err
	}
	if op != controllerutil.OperationResultNone {
		r.log.Info("Managed object successfully reconciled",
			logFieldObject, reconcilerRef.String(),
			logFieldKind, "ServiceAccount",
			executedOperation, op)
	}
	return childSARef, nil
}

type mutateFn func(client.Object) error

func (r *reconcilerBase) upsertDeployment(ctx context.Context, reconcilerRef types.NamespacedName, labelMap map[string]string, mutateObject mutateFn) (*appsv1.Deployment, controllerutil.OperationResult, error) {
	reconcilerDeployment := &appsv1.Deployment{}
	if err := parseDeployment(reconcilerDeployment); err != nil {
		return nil, controllerutil.OperationResultNone, errors.Wrap(err, "failed to parse reconciler Deployment manifest from ConfigMap")
	}

	reconcilerDeployment.Name = reconcilerRef.Name
	reconcilerDeployment.Namespace = reconcilerRef.Namespace

	// Add common deployment labels.
	// This enables label selecting deployment by R*Sync name & namespace.
	r.addLabels(reconcilerDeployment, labelMap)

	// Add common deployment labels to the pod template.
	// This enables label selecting deployment pods by R*Sync name & namespace.
	r.addTemplateLabels(reconcilerDeployment, labelMap)

	// Add deployment name to the pod template.
	// This enables label selecting deployment pods by deployment name.
	r.addTemplateLabels(reconcilerDeployment, map[string]string{
		metadata.DeploymentNameLabel: reconcilerRef.Name,
	})

	// Add deployment name to the pod selector.
	// This enables label selecting deployment pods by deployment name.
	r.addSelectorLabels(reconcilerDeployment, map[string]string{
		metadata.DeploymentNameLabel: reconcilerRef.Name,
	})

	if err := mutateObject(reconcilerDeployment); err != nil {
		return nil, controllerutil.OperationResultNone, err
	}
	result, err := r.createOrPatchDeployment(ctx, reconcilerDeployment)
	return reconcilerDeployment, result, err
}

// createOrPatchDeployment() first call Get() on the object. If the
// object does not exist, Create() will be called. If it does exist, Patch()
// will be called.
func (r *reconcilerBase) createOrPatchDeployment(ctx context.Context, declared *appsv1.Deployment) (controllerutil.OperationResult, error) {
	dRef := client.ObjectKeyFromObject(declared)
	kind := "Deployment"

	current := &appsv1.Deployment{}

	if err := r.client.Get(ctx, dRef, current); err != nil {
		if !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		r.log.V(3).Info("Managed object not found, creating",
			logFieldObject, dRef.String(),
			logFieldKind, kind)
		if err := r.client.Create(ctx, declared); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	// TODO: check if VPA is enabled
	vpaEnabled := false

	if r.isAutopilotCluster == nil {
		isAutopilot, err := util.IsGKEAutopilotCluster(r.client)
		if err != nil {
			return controllerutil.OperationResultNone, fmt.Errorf("unable to determine if it is an Autopilot cluster: %w", err)
		}
		r.isAutopilotCluster = &isAutopilot
	}

	adjusted, err := adjustContainerResources(vpaEnabled, *r.isAutopilotCluster, declared, current)
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	if adjusted {
		mutator := "VPA"
		if !vpaEnabled {
			mutator = "Autopilot"
		}
		r.log.V(3).Info("Managed object container resources updated",
			logFieldObject, dRef.String(),
			logFieldKind, kind,
			"mutator", mutator)
	}

	if reflect.DeepEqual(current.Labels, declared.Labels) && reflect.DeepEqual(current.Spec, declared.Spec) {
		return controllerutil.OperationResultNone, nil
	}

	r.log.V(3).Info("Managed object found, updating",
		logFieldObject, dRef.String(),
		logFieldKind, kind)
	if err := r.client.Update(ctx, declared); err != nil {
		// Let the next reconciliation retry the patch operation for valid request.
		if !apierrors.IsInvalid(err) {
			return controllerutil.OperationResultNone, err
		}
		// The provided data is invalid (e.g. http://b/196922619), so delete and re-create the resource.
		// This handles changes to immutable fields, like labels.
		r.log.Error(err, "Managed object update failed, deleting and re-creating",
			logFieldObject, dRef.String(),
			logFieldKind, kind)
		if err := r.client.Delete(ctx, declared); err != nil {
			return controllerutil.OperationResultNone, err
		}
		if err := r.client.Create(ctx, declared); err != nil {
			return controllerutil.OperationResultNone, err
		}
	}

	return controllerutil.OperationResultUpdated, nil
}

// adjustContainerResources adjusts the resources of all containers in the declared Deployment.
// It returns a boolean to indicate if the declared Deployment is updated or not.
// This function aims to address the fight among Autopilot, VPA and the reconciler since they all update the resources.
// Below is the resolution:
// 1. If VPA is enabled (no matter if it is a standard cluster or an Autopilot cluster),
//    use the current resources and ignore the override defined in the declared Deployment
//    because VPA will scale up and down automatically.
// 2. If VPA is not enabled, and the cluster is a standard cluster, the controller
//    will adjust the declared resources by applying the resource override.
// 3. If VPA is not enabled, and it is an Autopilot cluster, the controller will
//    honor the resource override, but update the declared resources to be compliant
//    with the Autopilot resource range constraints.
func adjustContainerResources(vpaEnabled, isAutopilot bool, declared, current *appsv1.Deployment) (bool, error) {
	// If VPA is enabled, use the current resources because VPA takes full control of them.
	if vpaEnabled {
		return keepCurrentContainerResources(declared, current), nil
	}

	// If it is NOT an Autopilot cluster, use the declared Deployment without adjustment.
	if !isAutopilot {
		return false, nil
	}

	resourceMutationAnnotation, hasResourceMutationAnnotation := current.Annotations[metadata.AutoPilotAnnotation]
	// If the current Deployment has not been adjusted by Autopilot yet, no adjustment to the declared Deployment is needed.
	// The controller will apply the resource override and the next reconciliation can handle the compliance update.
	if !hasResourceMutationAnnotation {
		return false, nil
	}

	// If the current Deployment has been adjusted by Autopilot, adjust the declared Deployment
	// to make sure the resource override is compliant with Autopilot constraints.
	resourcesChanged := false
	// input describes the containers' resources before Autopilot adjustment, output describes the resources after Autopilot adjustment.
	input, output, err := util.AutopilotResourceMutation(resourceMutationAnnotation)
	if err != nil {
		return false, fmt.Errorf("unable to marshal the resource mutation annotation: %w", err)

	}
	allRequestsNoLowerThanInput := true
	allRequestsNoHigherThanOutput := true
	for _, declaredContainer := range declared.Spec.Template.Spec.Containers {
		inputRequest := input[declaredContainer.Name].Requests
		outputRequest := output[declaredContainer.Name].Requests
		if declaredContainer.Resources.Requests.Cpu().Cmp(*inputRequest.Cpu()) < 0 || declaredContainer.Resources.Requests.Memory().Cmp(*inputRequest.Memory()) < 0 {
			allRequestsNoLowerThanInput = false
			break
		}
		if declaredContainer.Resources.Requests.Cpu().Cmp(*outputRequest.Cpu()) > 0 || declaredContainer.Resources.Requests.Memory().Cmp(*outputRequest.Memory()) > 0 {
			allRequestsNoHigherThanOutput = false
			break
		}
	}

	if allRequestsNoLowerThanInput && allRequestsNoHigherThanOutput {
		// No action is needed because Autopilot already optimized it based on the override
		resourcesChanged = keepCurrentContainerResources(declared, current)
	}

	// Add the autopilot annotation to the declared Deployment
	if declared.Annotations == nil {
		declared.Annotations = map[string]string{}
	}
	declared.Annotations[metadata.AutoPilotAnnotation] = resourceMutationAnnotation
	return resourcesChanged, nil
}

// keepCurrentContainerResources copies over all containers' resources from the current Deployment to the declared one,
// so that the discrepancy won't cause a Deployment update.
// That implies any resource override applied to the declared Deployment will be ignored.
// It returns a boolean to indicate if the declared Deployment is updated or not.
func keepCurrentContainerResources(declared, current *appsv1.Deployment) bool {
	resourceChanged := false
	for _, existingContainer := range current.Spec.Template.Spec.Containers {
		for i, desiredContainer := range declared.Spec.Template.Spec.Containers {
			if existingContainer.Name == desiredContainer.Name &&
				!reflect.DeepEqual(declared.Spec.Template.Spec.Containers[i].Resources, existingContainer.Resources) {
				declared.Spec.Template.Spec.Containers[i].Resources = existingContainer.Resources
				resourceChanged = true
			}
		}
	}
	return resourceChanged
}

// deployment returns the deployment from the server
func (r *reconcilerBase) deployment(ctx context.Context, dRef client.ObjectKey) (*appsv1.Deployment, error) {
	var depObj appsv1.Deployment
	if err := r.client.Get(ctx, dRef, &depObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Errorf(
				"Deployment %s not found in namespace: %s.", dRef.Name, dRef.Namespace)
		}
		return nil, errors.Wrapf(err, "error while retrieving deployment")
	}
	return &depObj, nil
}

func mutateContainerResource(ctx context.Context, c *corev1.Container, override v1beta1.OverrideSpec, reconcilerType string) {
	for _, override := range override.Resources {
		if override.ContainerName == c.Name {
			if !override.CPURequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceCPU] = override.CPURequest
				metrics.RecordResourceOverrideCount(ctx, reconcilerType, c.Name, "cpu")
			}
			if !override.CPULimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceCPU] = override.CPULimit
				metrics.RecordResourceOverrideCount(ctx, reconcilerType, c.Name, "cpu")
			}
			if !override.MemoryRequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceMemory] = override.MemoryRequest
				metrics.RecordResourceOverrideCount(ctx, reconcilerType, c.Name, "memory")
			}
			if !override.MemoryLimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceMemory] = override.MemoryLimit
				metrics.RecordResourceOverrideCount(ctx, reconcilerType, c.Name, "memory")
			}
		}
	}
}

// addLabels will copy the content of labelMaps to the current resource labels
func (r *reconcilerBase) addLabels(resource client.Object, labelMap map[string]string) {
	currentLabels := resource.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}

	for key, value := range labelMap {
		currentLabels[key] = value
	}

	resource.SetLabels(currentLabels)

}

func (r *reconcilerBase) injectFleetWorkloadIdentityCredentials(podTemplate *corev1.PodTemplateSpec, gsaEmail string) error {
	content := map[string]interface{}{
		"type":                              "external_account",
		"audience":                          fmt.Sprintf("identitynamespace:%s:%s", r.membership.Spec.WorkloadIdentityPool, r.membership.Spec.IdentityProvider),
		"service_account_impersonation_url": fmt.Sprintf("https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/%s:generateAccessToken", gsaEmail),
		"subject_token_type":                "urn:ietf:params:oauth:token-type:jwt",
		"token_url":                         "https://sts.googleapis.com/v1/token",
		"credential_source": map[string]string{
			"file": filepath.Join(gcpKSATokenDir, gsaTokenPath),
		},
	}
	bytes, err := json.Marshal(content)
	if err != nil {
		return errors.Wrap(err, "failed to marshal the Fleet Workload Identity credentials")
	}
	core.SetAnnotation(podTemplate, metadata.FleetWorkloadIdentityCredentials, string(bytes))
	return nil
}

// addSelectorLabels will merge the labelMaps into the deployment spec.selector.matchLabels
func (r *reconcilerBase) addSelectorLabels(deployment *appsv1.Deployment, labelMap map[string]string) {
	currentLabels := deployment.Spec.Selector.MatchLabels
	if currentLabels == nil {
		currentLabels = make(map[string]string, len(labelMap))
	}

	for key, value := range labelMap {
		currentLabels[key] = value
	}

	deployment.Spec.Selector.MatchLabels = currentLabels
}

// addTemplateLabels will merge the labelMaps into the deployment spec.template.labels
func (r *reconcilerBase) addTemplateLabels(deployment *appsv1.Deployment, labelMap map[string]string) {
	currentLabels := deployment.Spec.Template.Labels
	if currentLabels == nil {
		currentLabels = make(map[string]string, len(labelMap))
	}

	for key, value := range labelMap {
		currentLabels[key] = value
	}

	deployment.Spec.Template.Labels = currentLabels
}

// addTypeInformationToObject adds TypeMeta information to a runtime.Object based upon the loaded scheme.Scheme
// inspired by: https://github.com/kubernetes/cli-runtime/blob/v0.19.2/pkg/printers/typesetter.go#L41.
// Note: The function only works for GVKs registered with the local scheme with exact version matching.
func (r *reconcilerBase) addTypeInformationToObject(obj runtime.Object) error {
	gvks, _, err := r.scheme.ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}

	for _, gvk := range gvks {
		if len(gvk.Kind) == 0 {
			continue
		}
		if len(gvk.Version) == 0 || gvk.Version == runtime.APIVersionInternal {
			continue
		}
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		break
	}

	return nil
}
