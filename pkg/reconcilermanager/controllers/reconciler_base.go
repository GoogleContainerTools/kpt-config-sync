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
	"strings"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
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

	logFieldSyncKind        = "syncKind"
	logFieldSyncRef         = "sync"
	logFieldObjectKind      = "objectKind"
	logFieldObjectRef       = "object"
	logFieldOperation       = "operation"
	logFieldObjectStatus    = "objectStatus"
	logFieldReconciler      = "reconciler"
	logFieldResourceVersion = "resourceVersion"
)

// The fields in reconcilerManagerAllowList are the fields that reconciler manager allow
// users or other controllers to modify.
var reconcilerManagerAllowList = []string{
	"$.spec.template.spec.containers[*].terminationMessagePath",
	"$.spec.template.spec.containers[*].terminationMessagePolicy",
	"$.spec.template.spec.containers[*].*.timeoutSeconds",
	"$.spec.template.spec.containers[*].*.periodSeconds",
	"$.spec.template.spec.containers[*].*.successThreshold",
	"$.spec.template.spec.containers[*].*.failureThreshold",
	"$.spec.template.spec.tolerations",
	"$.spec.template.spec.restartPolicy",
	"$.spec.template.spec.nodeSelector", // remove this after Config Sync support user-defined nodeSelector through API
	"$.spec.template.spec.terminationGracePeriodSeconds",
	"$.spec.template.spec.dnsPolicy",
	"$.spec.template.spec.schedulerName",
	"$.spec.revisionHistoryLimit",
	"$.spec.progressDeadlineSeconds",
}

// reconcilerBase provides common data and methods for the RepoSync and RootSync reconcilers
type reconcilerBase struct {
	loggingController

	clusterName                  string
	client                       client.Client    // caching
	watcher                      client.WithWatch // non-caching
	dynamicClient                dynamic.Interface
	scheme                       *runtime.Scheme
	isAutopilotCluster           *bool
	reconcilerPollingPeriod      time.Duration
	hydrationPollingPeriod       time.Duration
	helmSyncVersionPollingPeriod time.Duration
	membership                   *hubv1.Membership

	// syncKind is the kind of the sync object: RootSync or RepoSync.
	syncKind string
}

const volumeNameLengthLimit = 63

func (r *reconcilerBase) serviceAccountSubject(reconcilerRef types.NamespacedName) rbacv1.Subject {
	return newSubject(reconcilerRef.Name, reconcilerRef.Namespace, kinds.ServiceAccount().Kind)
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

	op, err := CreateOrUpdate(ctx, r.client, childSA, func() error {
		r.addLabels(childSA, labelMap)
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
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, childSARef.String(),
			logFieldObjectKind, "ServiceAccount",
			logFieldOperation, op)
	}
	return childSARef, nil
}

type mutateFn func(client.Object) error

func (r *reconcilerBase) upsertDeployment(ctx context.Context, reconcilerRef types.NamespacedName, labelMap map[string]string, mutateObject mutateFn) (*unstructured.Unstructured, controllerutil.OperationResult, error) {
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
	appliedObj, op, err := r.createOrPatchDeployment(ctx, reconcilerDeployment)

	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, reconcilerRef.String(),
			logFieldObjectKind, "Deployment",
			logFieldOperation, op)
	}
	return appliedObj, op, err
}

// createOrPatchDeployment() first call Get() on the object. If the
// object does not exist, Create() will be called. If it does exist, Patch()
// will be called.
func (r *reconcilerBase) createOrPatchDeployment(ctx context.Context, declared *appsv1.Deployment) (*unstructured.Unstructured, controllerutil.OperationResult, error) {
	id := core.ID{
		ObjectKey: client.ObjectKeyFromObject(declared),
		GroupKind: kinds.Deployment().GroupKind(),
	}
	forcePatch := true
	deploymentClient := r.dynamicClient.Resource(kinds.DeploymentResource()).Namespace(id.Namespace)
	currentDeploymentUnstructured, err := deploymentClient.Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationGet)
		}
		r.logger(ctx).V(3).Info("Managed object not found, creating",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)
		data, err := json.Marshal(declared)
		if err != nil {
			return nil, controllerutil.OperationResultNone, fmt.Errorf("failed to marshal declared deployment object to byte array: %w", err)
		}
		appliedObj, err := deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: reconcilermanager.ManagerName, Force: &forcePatch})
		if err != nil {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationPatch)
		}
		return appliedObj, controllerutil.OperationResultCreated, nil
	}
	currentGeneration := currentDeploymentUnstructured.GetGeneration()
	currentUID := currentDeploymentUnstructured.GetUID()

	if r.isAutopilotCluster == nil {
		isAutopilot, err := util.IsGKEAutopilotCluster(r.client)
		if err != nil {
			return nil, controllerutil.OperationResultNone, fmt.Errorf("unable to determine if it is an Autopilot cluster: %w", err)
		}
		r.isAutopilotCluster = &isAutopilot
	}
	dep, err := compareDeploymentsToCreatePatchData(*r.isAutopilotCluster, declared, currentDeploymentUnstructured, reconcilerManagerAllowList, r.scheme)
	if err != nil {
		return nil, controllerutil.OperationResultNone, err
	}
	if dep.adjusted {
		mutator := "Autopilot"
		r.logger(ctx).V(3).Info("Managed object container resources updated",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind,
			"mutator", mutator)
	}
	if dep.same {
		return nil, controllerutil.OperationResultNone, nil
	}
	r.logger(ctx).V(3).Info("Managed object found, patching",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	appliedObj, err := deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, dep.dataToPatch, metav1.PatchOptions{FieldManager: reconcilermanager.ManagerName, Force: &forcePatch})
	if err != nil {
		// Let the next reconciliation retry the patch operation for valid request.
		if !apierrors.IsInvalid(err) {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationPatch)
		}
		// The provided data is invalid (e.g. http://b/196922619), so delete and re-create the resource.
		// This handles changes to immutable fields, like labels.
		r.logger(ctx).Error(err, "Managed object update failed, deleting and re-creating",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)
		if err := deploymentClient.Delete(ctx, id.Name, metav1.DeleteOptions{}); err != nil {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationDelete)
		}
		data, err := json.Marshal(declared)
		if err != nil {
			return nil, controllerutil.OperationResultNone, fmt.Errorf("failed to marshal declared deployment object to byte array: %w", err)
		}
		appliedObj, err = deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: reconcilermanager.ManagerName, Force: &forcePatch})
		if err != nil {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationPatch)
		}
	}
	if appliedObj.GetGeneration() == currentGeneration && appliedObj.GetUID() == currentUID {
		return appliedObj, controllerutil.OperationResultNone, nil
	}
	return appliedObj, controllerutil.OperationResultUpdated, nil
}

// deleteDeploymentFields delete all the fields in allowlist from unstructured object and convert the unstructured object to Deployment object
func deleteDeploymentFields(allowList []string, unstructuredDeployment *unstructured.Unstructured) (*appsv1.Deployment, error) {
	for _, path := range allowList {
		if err := deleteFields(unstructuredDeployment.Object, path); err != nil {
			return nil, err
		}
	}
	var resultDeployment appsv1.Deployment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredDeployment.Object, &resultDeployment); err != nil {
		return nil, fmt.Errorf("failed to convert from current reconciler unstructured object to deployment object: %w", err)
	}
	return &resultDeployment, nil
}

type deploymentProcessResult struct {
	same        bool
	adjusted    bool
	dataToPatch []byte
}

// compareDeploymentsToCreatePatchData checks if current deployment is same with declared deployment when ignore the fields in allowlist. If not, it creates a byte array used for PATCH later
func compareDeploymentsToCreatePatchData(isAutopilot bool, declared *appsv1.Deployment, currentDeploymentUnstructured *unstructured.Unstructured, allowList []string, scheme *runtime.Scheme) (*deploymentProcessResult, error) {
	processedCurrent, err := deleteDeploymentFields(allowList, currentDeploymentUnstructured)
	if err != nil {
		return &deploymentProcessResult{}, err
	}
	adjusted, err := adjustContainerResources(isAutopilot, declared, processedCurrent)
	if err != nil {
		return &deploymentProcessResult{}, err
	}

	unObjDeclared, err := kinds.ToUnstructured(declared, scheme)
	if err != nil {
		return &deploymentProcessResult{}, err
	}
	processedDeclared, err := deleteDeploymentFields(allowList, unObjDeclared)
	if err != nil {
		return &deploymentProcessResult{}, err
	}
	if equality.Semantic.DeepEqual(processedCurrent.Labels, processedDeclared.Labels) && equality.Semantic.DeepEqual(processedCurrent.Spec, processedDeclared.Spec) {
		return &deploymentProcessResult{true, adjusted, nil}, nil
	}
	data, err := json.Marshal(unObjDeclared)
	if err != nil {
		return &deploymentProcessResult{}, err
	}
	return &deploymentProcessResult{false, adjusted, data}, nil
}

// adjustContainerResources adjusts the resources of all containers in the declared Deployment.
// It returns a boolean to indicate if the declared Deployment is updated or not.
// This function aims to address the fight among Autopilot and the reconciler since they all update the resources.
// Below is the resolution:
//
//  1. If  the cluster is a standard cluster, the controller
//     will adjust the declared resources by applying the resource override.
//
//  2. If it is an Autopilot cluster, the controller will
//     honor the resource override, but update the declared resources to be compliant
//     with the Autopilot resource range constraints.
func adjustContainerResources(isAutopilot bool, declared, current *appsv1.Deployment) (bool, error) {
	// If it is NOT an Autopilot cluster, use the declared Deployment without adjustment.
	if !isAutopilot {
		return false, nil
	}

	resourceMutationAnnotation, hasResourceMutationAnnotation := current.Annotations[metadata.AutoPilotAnnotation]
	// If the current Deployment has not been adjusted by Autopilot yet, no adjustment to the declared Deployment is needed.
	// The controller will apply the resource override and the next reconciliation can handle the compliance update.
	if !hasResourceMutationAnnotation || len(resourceMutationAnnotation) == 0 {
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
		if declaredContainer.Resources.Requests == nil {
			continue // for the containers like gcenode-askpass-sidecar which does not have resource request defined, skip the check
		}
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
				!equality.Semantic.DeepEqual(declared.Spec.Template.Spec.Containers[i].Resources, existingContainer.Resources) {
				declared.Spec.Template.Spec.Containers[i].Resources = existingContainer.Resources
				resourceChanged = true
			}
		}
	}
	return resourceChanged
}

// deployment returns the deployment from the server
func (r *reconcilerBase) deployment(ctx context.Context, dRef client.ObjectKey) (*unstructured.Unstructured, error) {
	deployObj, err := r.dynamicClient.Resource(kinds.DeploymentResource()).Namespace(dRef.Namespace).Get(ctx, dRef.Name, metav1.GetOptions{})
	if err != nil {
		id := core.ID{ObjectKey: dRef, GroupKind: kinds.Deployment().GroupKind()}
		return nil, NewObjectOperationErrorWithID(err, id, OperationGet)
	}
	return deployObj, nil
}

func mountConfigMapValuesFiles(templateSpec *corev1.PodSpec, c *corev1.Container, valuesFrom []v1beta1.ValuesFileSources) {
	var valuesFiles []string

	for i, vf := range valuesFrom {
		fileName := reconcilermanager.HelmConfigMapRef
		mountPath := filepath.Join("/etc/config", vf.Name, vf.ValuesFile)
		valuesFiles = append(valuesFiles, filepath.Join(mountPath, fileName))
		volumeName := fmt.Sprintf("vol-%d-", i) + strings.ToLower(vf.Name)

		if len(volumeName) > volumeNameLengthLimit {
			// trim the volume name as we only have a length limit of 63, even though
			// the ConfigMap name can be much longer.  The prefix
			// containing the index i should be sufficient to uniquify the value.
			volumeName = volumeName[:volumeNameLengthLimit]
		}

		templateSpec.Volumes = append(templateSpec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vf.Name,
					},
					Items: []corev1.KeyToPath{{
						Key:  vf.ValuesFile,
						Path: fileName,
					}},
				},
			},
		})
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
		})
	}

	if len(valuesFiles) > 0 {
		c.Env = append(c.Env, corev1.EnvVar{
			Name:  reconcilermanager.HelmValuesFileSources,
			Value: strings.Join(valuesFiles, ","),
		})
	}
}

func mutateContainerResource(c *corev1.Container, override *v1beta1.OverrideSpec) {
	if override == nil {
		return
	}

	for _, override := range override.Resources {
		if override.ContainerName == c.Name {
			if !override.CPURequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceCPU] = override.CPURequest
			}
			if !override.CPULimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceCPU] = override.CPULimit
			}
			if !override.MemoryRequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceMemory] = override.MemoryRequest
			}
			if !override.MemoryLimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceMemory] = override.MemoryLimit
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

// validateCACertSecret verify that caCertSecretRef is well formed with a key named "cert"
func (r *reconcilerBase) validateCACertSecret(ctx context.Context, namespace, caCertSecretRefName string) error {
	if useCACert(caCertSecretRefName) {
		secret, err := validateSecretExist(ctx,
			caCertSecretRefName,
			namespace,
			r.client)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return errors.Errorf("Secret %s not found, create one to allow client connections with CA certificate", caCertSecretRefName)
			}
			return errors.Wrapf(err, "Secret %s get failed", caCertSecretRefName)
		}
		if _, ok := secret.Data[CACertSecretKey]; !ok {
			return fmt.Errorf("caCertSecretRef was set, but %s key is not present in %s Secret", CACertSecretKey, caCertSecretRefName)
		}
	}
	return nil
}

// addTypeInformationToObject looks up and adds GVK to a runtime.Object based upon the loaded Scheme
func (r *reconcilerBase) addTypeInformationToObject(obj runtime.Object) error {
	gvk, err := kinds.Lookup(obj, r.scheme)
	if err != nil {
		return fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	return nil
}

// setupOrTeardown handles the following lifecycle steps:
//
// - when deletionTimestamp IS NOT set...
//   - add the ReconcilerManagerFinalizer
//   - execute setupFn
//
// - when deletionTimestamp IS set...
//   - wait for ReconcilerFinalizer to be removed (return and wait for next event)
//   - execute teardownFn when the finalizer is set
//   - remove the ReconcilerManagerFinalizer once teardownFn is successful
//
// A finalizer is used instead of the Kubernetes' built-in garbage collection,
// because the Kubernetes garbage collector does not allow cross-namespace owner
// references. Using the finalizer for all resources also ensures well ordered
// deletion, which allows for stricter contracts when uninstalling.
func (r *reconcilerBase) setupOrTeardown(ctx context.Context, syncObj client.Object, setupFn, teardownFn func(context.Context) error) error {
	if syncObj.GetDeletionTimestamp().IsZero() {
		// The object is NOT being deleted.
		if !controllerutil.ContainsFinalizer(syncObj, metadata.ReconcilerManagerFinalizer) {
			// The object is new and doesn't have our finalizer yet.
			// Add our finalizer and update the object.
			controllerutil.AddFinalizer(syncObj, metadata.ReconcilerManagerFinalizer)
			if err := r.client.Update(ctx, syncObj); err != nil {
				err = status.APIServerError(err,
					fmt.Sprintf("failed to update %s to add finalizer", r.syncKind))
				r.logger(ctx).Error(err, "Finalizer injection failed")
				return err
			}
			r.logger(ctx).Info("Finalizer injection successful")
		}

		// Our finalizer is present, so setup managed resource objects.
		if err := setupFn(ctx); err != nil {
			return errors.Wrap(err, "setup failed")
		}
		return nil
	}
	// Else - the object is being deleted.

	klog.Infoln("checking finalizers")
	if controllerutil.ContainsFinalizer(syncObj, metadata.ReconcilerFinalizer) {
		// The object is being deleted, but the reconciler finalizer is still running.
		// Wait for the reconciler finalizer to complete.
		klog.Infoln("")
		r.logger(ctx).Info("Waiting for Reconciler Finalizer to finish")
		return nil
	}

	if controllerutil.ContainsFinalizer(syncObj, metadata.ReconcilerManagerFinalizer) {
		// Our finalizer is present, so delete managed resource objects.
		if err := teardownFn(ctx); err != nil {
			return errors.Wrap(err, "teardown failed")
		}

		// Remove our finalizer and update the object.
		controllerutil.RemoveFinalizer(syncObj, metadata.ReconcilerManagerFinalizer)
		if err := r.client.Update(ctx, syncObj); err != nil {
			err = status.APIServerError(err,
				fmt.Sprintf("failed to update %s to remove finalizer", r.syncKind))
			r.logger(ctx).Error(err, "Finalizer removal failed")
			return err
		}
		r.logger(ctx).Info("Finalizer removal successful")
	}

	return nil
}
