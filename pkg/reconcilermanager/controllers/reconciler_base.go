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
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/applyset"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	webhookconfiguration "kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
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

// The fields in reconcilerManagerAllowList are the fields that reconciler manager
// allows users or other controllers to modify on the reconciler Deployment.
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
	reconcilerFinalizerHandler reconcilerFinalizerHandler

	clusterName             string
	client                  client.Client    // caching
	watcher                 client.WithWatch // non-caching
	dynamicClient           dynamic.Interface
	scheme                  *runtime.Scheme
	autopilot               *bool
	reconcilerPollingPeriod time.Duration
	hydrationPollingPeriod  time.Duration
	membership              *hubv1.Membership
	knownHostExist          bool
	githubApp               githubAppSpec
	webhookEnabled          bool

	// syncGVK is the GroupVersionKind of the sync object: RootSync or RepoSync.
	syncGVK schema.GroupVersionKind

	// controllerName is used by tests to de-dupe controllers
	controllerName string
}

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
		core.AddLabels(childSA, labelMap)
		// Update ownerRefs for RootSync ServiceAccount.
		// Do not set ownerRefs for RepoSync ServiceAccount, since Reconciler Manager,
		// performs garbage collection for Reposync controller resources.
		if len(refs) > 0 {
			childSA.OwnerReferences = refs
		}
		if auth == configsync.AuthGCPServiceAccount {
			// Set annotation when impersonating a GSA on a Workload Identity enabled cluster.
			core.SetAnnotation(childSA, GCPSAAnnotationKey, email)
		} else {
			// Remove the annotation when not impersonating a GSA
			core.RemoveAnnotations(childSA, GCPSAAnnotationKey)
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
		return nil, controllerutil.OperationResultNone, fmt.Errorf("failed to parse reconciler Deployment manifest from ConfigMap: %w", err)
	}

	reconcilerDeployment.Name = reconcilerRef.Name
	reconcilerDeployment.Namespace = reconcilerRef.Namespace

	// Add common deployment labels.
	// This enables label selecting deployment by R*Sync name & namespace.
	core.AddLabels(reconcilerDeployment, labelMap)

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
	appliedObj, op, err := r.applyDeployment(ctx, reconcilerDeployment)

	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, reconcilerRef.String(),
			logFieldObjectKind, "Deployment",
			logFieldOperation, op)
	}
	return appliedObj, op, err
}

// applyDeployment applies the declared deployment.
// If it exists before apply, and has the GKE Autopilot adjustment annotation,
// then the declared deployment is modified to match the resource adjustments,
// to avoid fighting with the autopilot mutating webhook.
func (r *reconcilerBase) applyDeployment(ctx context.Context, declared *appsv1.Deployment) (*unstructured.Unstructured, controllerutil.OperationResult, error) {
	id := core.ID{
		ObjectKey: client.ObjectKeyFromObject(declared),
		GroupKind: kinds.Deployment().GroupKind(),
	}
	patchOpts := metav1.PatchOptions{FieldManager: reconcilermanager.ManagerName, Force: ptr.To(true)}
	deploymentClient := r.dynamicClient.Resource(kinds.DeploymentResource()).Namespace(id.Namespace)
	currentDeploymentUnstructured, err := deploymentClient.Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationGet)
		}
		r.logger(ctx).Info("Managed object not found, creating",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)
		// Ensure GVK is included in the patch data
		declared.SetGroupVersionKind(kinds.Deployment())
		data, err := json.Marshal(declared)
		if err != nil {
			return nil, controllerutil.OperationResultNone, fmt.Errorf("failed to marshal declared deployment object to byte array: %w", err)
		}
		appliedObj, err := deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, data, patchOpts)
		if err != nil {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationPatch)
		}
		return appliedObj, controllerutil.OperationResultCreated, nil
	}
	currentGeneration := currentDeploymentUnstructured.GetGeneration()
	currentUID := currentDeploymentUnstructured.GetUID()

	dep, err := r.compareDeploymentsToCreatePatchData(declared, currentDeploymentUnstructured, reconcilerManagerAllowList)
	if err != nil {
		return nil, controllerutil.OperationResultNone, err
	}
	if dep.adjusted {
		mutator := "Autopilot"
		r.logger(ctx).Info("Managed object container resources adjusted by autopilot",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind,
			"mutator", mutator)
	}
	if dep.same {
		r.logger(ctx).Info("Managed object apply skipped, no diff",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)
		return nil, controllerutil.OperationResultNone, nil
	}
	r.logger(ctx).Info("Managed object found, patching",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind,
		"patchJSON", string(dep.dataToPatch))
	appliedObj, err := deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, dep.dataToPatch, patchOpts)
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
		// Ensure GVK is included in the patch data
		declared.SetGroupVersionKind(kinds.Deployment())
		data, err := json.Marshal(declared)
		if err != nil {
			return nil, controllerutil.OperationResultNone, fmt.Errorf("failed to marshal declared deployment object to byte array: %w", err)
		}
		appliedObj, err = deploymentClient.Patch(ctx, id.Name, types.ApplyPatchType, data, patchOpts)
		if err != nil {
			return nil, controllerutil.OperationResultNone, NewObjectOperationErrorWithID(err, id, OperationPatch)
		}
	}
	if appliedObj.GetGeneration() == currentGeneration && appliedObj.GetUID() == currentUID {
		return appliedObj, controllerutil.OperationResultNone, nil
	}
	return appliedObj, controllerutil.OperationResultUpdated, nil
}

func (r *reconcilerBase) isAutopilot() (bool, error) {
	if r.autopilot != nil {
		return *r.autopilot, nil
	}
	autopilot, err := util.IsGKEAutopilotCluster(r.client)
	if err != nil {
		return false, fmt.Errorf("unable to determine if it is an Autopilot cluster: %w", err)
	}
	r.autopilot = &autopilot
	return autopilot, nil
}

// deleteDeploymentFields delete all the fields in allowlist from unstructured object and convert the unstructured object to Deployment object
func deleteDeploymentFields(allowList []string, unstructuredDeployment *unstructured.Unstructured) (*appsv1.Deployment, error) {
	deploymentDeepCopy := unstructuredDeployment.DeepCopy()
	for _, path := range allowList {
		if err := deleteFields(deploymentDeepCopy.Object, path); err != nil {
			return nil, err
		}
	}
	var resultDeployment appsv1.Deployment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentDeepCopy.Object, &resultDeployment); err != nil {
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
func (r *reconcilerBase) compareDeploymentsToCreatePatchData(declared *appsv1.Deployment, currentDeploymentUnstructured *unstructured.Unstructured, allowList []string) (*deploymentProcessResult, error) {
	isAutopilot, err := r.isAutopilot()
	if err != nil {
		return nil, err
	}
	processedCurrent, err := deleteDeploymentFields(allowList, currentDeploymentUnstructured)
	if err != nil {
		return nil, err
	}
	adjusted, err := adjustContainerResources(isAutopilot, declared, processedCurrent)
	if err != nil {
		return nil, err
	}
	uObjDeclared, err := kinds.ToUnstructured(declared, r.scheme)
	if err != nil {
		return nil, err
	}
	processedDeclared, err := deleteDeploymentFields(allowList, uObjDeclared)
	if err != nil {
		return nil, err
	}
	if equality.Semantic.DeepEqual(processedCurrent.Labels, processedDeclared.Labels) && equality.Semantic.DeepEqual(processedCurrent.Spec, processedDeclared.Spec) {
		return &deploymentProcessResult{true, adjusted, nil}, nil
	}
	data, err := json.Marshal(uObjDeclared)
	if err != nil {
		return nil, err
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

// mountConfigMapValuesFiles mounts the helm values files from the referenced ConfigMaps as files in the helm-sync
// container.
func mountConfigMapValuesFiles(templateSpec *corev1.PodSpec, c *corev1.Container, valuesFileRefs []v1beta1.ValuesFileRef) {
	var valuesFiles []string

	for i, vf := range valuesFileRefs {
		dataKey := validate.HelmValuesFileDataKeyOrDefault(vf.DataKey)
		mountPath := filepath.Join("/etc/config", fmt.Sprintf("helm_values_file_path_%d", i))
		fileName := filepath.Join(vf.Name, dataKey)
		valuesFiles = append(valuesFiles, filepath.Join(mountPath, fileName))
		volumeName := fmt.Sprintf("valuesfile-vol-%d", i)
		templateSpec.Volumes = append(templateSpec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vf.Name,
					},
					Items: []corev1.KeyToPath{{
						Key:  dataKey,
						Path: fileName,
					}},
					// The ConfigMap may be deleted before the RSync. To prevent the reconciler
					// pod from going into an error state when that happens, we must mark
					// this ConfigMap mount as optional and have our validation checks elsewhere.
					Optional: ptr.To(true),
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
			Name:  reconcilermanager.HelmValuesFilePaths,
			Value: strings.Join(valuesFiles, ","),
		})
	}
}

func removeArg(args []string, i int) []string {
	if i == 0 {
		// remove first arg
		args = args[i+1:]
	} else if i == len(args)-1 {
		// remove last arg
		args = args[:i]
	} else {
		// remove middle arg
		args = append(args[:i], args[i+1:]...)
	}
	return args
}

// BuildFWICredsContent generates the Fleet WI credentials content in a JSON string.
func BuildFWICredsContent(workloadIdentityPool, identityProvider, gsaEmail string, authType configsync.AuthType) (string, error) {
	content := map[string]interface{}{
		"type":               "external_account",
		"audience":           fmt.Sprintf("identitynamespace:%s:%s", workloadIdentityPool, identityProvider),
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"token_url":          "https://sts.googleapis.com/v1/token",
		"credential_source": map[string]string{
			"file": filepath.Join(gcpKSATokenDir, gsaTokenPath),
		},
	}
	// Set this field to get a GSA access token only when impersonating a GSA
	// Otherwise, skip this field to obtain a federated access token.
	if gsaEmail != "" && authType == configsync.AuthGCPServiceAccount {
		content["service_account_impersonation_url"] = fmt.Sprintf("https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/%s:generateAccessToken", gsaEmail)
	}
	bytes, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the Fleet Workload Identity credentials: %w", err)
	}
	return string(bytes), nil
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
				return fmt.Errorf("Secret %s not found, create one to allow client connections with CA certificate", caCertSecretRefName)
			}
			return fmt.Errorf("Secret %s get failed: %w", caCertSecretRefName, err)
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
		return fmt.Errorf("missing apiVersion or kind and cannot assign it: %w", err)
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
			if err := r.client.Update(ctx, syncObj, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
				err = status.APIServerError(err,
					fmt.Sprintf("failed to update %s to add finalizer", r.syncGVK.Kind))
				r.logger(ctx).Error(err, "Finalizer injection failed")
				return err
			}
			r.logger(ctx).Info("Finalizer injection successful")
		}

		// Our finalizer is present, so setup managed resource objects.
		if err := setupFn(ctx); err != nil {
			var nre *NoRetryError
			if errors.As(err, &nre) {
				r.logger(ctx).Error(err, "Setup failed (waiting for resource status to change)")
				// Return nil to avoid triggering immediate retry.
				return nil
			}
			return fmt.Errorf("setup failed: %w", err)
		}

		return nil
	}
	// Else - the object is being deleted.

	if controllerutil.ContainsFinalizer(syncObj, metadata.ReconcilerFinalizer) {
		removed, err := r.reconcilerFinalizerHandler.handleReconcilerFinalizer(ctx, syncObj)
		if err != nil {
			return err
		}
		if !removed {
			r.logger(ctx).Info("Waiting for Reconciler Finalizer to finish")
			return nil
		}
	}

	if controllerutil.ContainsFinalizer(syncObj, metadata.ReconcilerManagerFinalizer) {
		// Our finalizer is present, so delete managed resource objects.
		if err := teardownFn(ctx); err != nil {
			var nre *NoRetryError
			if errors.As(err, &nre) {
				r.logger(ctx).Error(err, "Teardown failed (waiting for resource status to change)")
				// Return nil to avoid triggering immediate retry.
				return nil
			}
			return fmt.Errorf("teardown failed: %w", err)
		}

		// Remove our finalizer and update the object.
		controllerutil.RemoveFinalizer(syncObj, metadata.ReconcilerManagerFinalizer)
		if err := r.client.Update(ctx, syncObj, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
			err = status.APIServerError(err,
				fmt.Sprintf("failed to update %s to remove the reconciler-manager finalizer", r.syncGVK.Kind))
			r.logger(ctx).Error(err, "Removal of reconciler-manager finalizer failed")
			return err
		}
		r.logger(ctx).Info("Removal of reconciler-manager finalizer succeeded")
	}

	return nil
}

// ManagedByLabel is a uniform label that is applied to all resources which are
// managed by reconciler-manager.
func ManagedByLabel() map[string]string {
	return map[string]string{
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}
}

// ManagedObjectLabelMap returns the standard labels applied to objects related
// to a RootSync/RepoSync that are created by reconciler-manager.
func ManagedObjectLabelMap(syncKind string, rsRef types.NamespacedName) map[string]string {
	labelMap := ManagedByLabel()
	labelMap[metadata.SyncNamespaceLabel] = rsRef.Namespace
	labelMap[metadata.SyncNameLabel] = rsRef.Name
	labelMap[metadata.SyncKindLabel] = syncKind
	return labelMap
}

func (r *reconcilerBase) updateRBACBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, binding client.Object) error {
	existingBinding := binding.DeepCopyObject()
	subjects := []rbacv1.Subject{r.serviceAccountSubject(reconcilerRef)}
	if crb, ok := binding.(*rbacv1.ClusterRoleBinding); ok {
		crb.Subjects = subjects
	} else if rb, ok := binding.(*rbacv1.RoleBinding); ok {
		rb.Subjects = subjects
	}
	core.AddLabels(binding, ManagedObjectLabelMap(r.syncGVK.Kind, rsRef))
	if equality.Semantic.DeepEqual(existingBinding, binding) {
		return nil
	}
	if err := r.client.Update(ctx, binding, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		return err
	}
	bindingNN := types.NamespacedName{
		Name:      binding.GetName(),
		Namespace: binding.GetNamespace(),
	}
	r.logger(ctx).Info("Managed object update successful",
		logFieldObjectRef, bindingNN.String(),
		logFieldObjectKind, binding.GetObjectKind().GroupVersionKind().Kind)
	return nil
}

func (r *reconcilerBase) upsertSharedClusterRoleBinding(ctx context.Context, name, clusterRole string, reconcilerRef, rsRef types.NamespacedName) error {
	crbRef := client.ObjectKey{Name: name}
	childCRB := &rbacv1.ClusterRoleBinding{}
	childCRB.Name = crbRef.Name

	labelMap := ManagedObjectLabelMap(r.syncGVK.Kind, rsRef)
	// Remove sync-name label since the ClusterRoleBinding may be shared
	delete(labelMap, metadata.SyncNameLabel)

	op, err := CreateOrUpdate(ctx, r.client, childCRB, func() error {
		core.AddLabels(childCRB, labelMap)
		childCRB.RoleRef = rolereference(clusterRole, "ClusterRole")
		childCRB.Subjects = addSubject(childCRB.Subjects, r.serviceAccountSubject(reconcilerRef))
		// Remove any existing OwnerReferences, now that we're using finalizers.
		childCRB.OwnerReferences = nil
		return nil
	})
	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, crbRef.String(),
			logFieldObjectKind, "ClusterRoleBinding",
			logFieldOperation, op)
	}
	return nil
}

func (r *reconcilerBase) isKnownHostsEnabled(auth configsync.AuthType) bool {
	if auth == configsync.AuthSSH && r.knownHostExist {
		return true
	}
	return false
}

func (r *reconcilerBase) isWebhookEnabled(ctx context.Context) (bool, error) {
	err := r.client.Get(ctx, client.ObjectKey{Name: webhookconfiguration.Name}, &admissionv1.ValidatingWebhookConfiguration{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// patchSyncMetadata patches RSync metadata:
// - Set label "applyset.kubernetes.io/id" (see applyset.IDFromSync for value format)
// - Set annotation "applyset.kubernetes.io/tooling" (see applyset.FormatTooling for value format)
//
// This method uses a merge-patch, instead of server-side apply, because the
// reconciler-manager does not own the RSync. No other fields should be modified.
//
// Returns true if changes were made, false if no changes were necessary.
//
// Returns an error if the tooling annotation exists with a different name or
// an invalid value.
func (r *reconcilerBase) patchSyncMetadata(ctx context.Context, rs client.Object) (bool, error) {
	syncScope := declared.ScopeFromSyncNamespace(rs.GetNamespace())
	applySetID := applyset.IDFromSync(rs.GetName(), syncScope)
	applySetToolingValue := applyset.FormatTooling(metadata.ApplySetToolingName, metadata.ApplySetToolingVersion)
	updateRequired := false

	currentApplySetID := core.GetLabel(rs, metadata.ApplySetParentIDLabel)
	if currentApplySetID != applySetID {
		updateRequired = true

		if r.logger(ctx).V(5).Enabled() {
			r.logger(ctx).Info("Updating sync metadata: applyset label",
				logFieldResourceVersion, rs.GetResourceVersion(),
				"labelKey", metadata.ApplySetParentIDLabel,
				"labelValueOld", currentApplySetID, "labelValue", applySetID)
		}
	}

	currentApplySetToolingValue := core.GetAnnotation(rs, metadata.ApplySetToolingAnnotation)
	if currentApplySetToolingValue != applySetToolingValue {
		updateRequired = true

		// If already set, validate that we can safely adopt the ApplySet
		if currentApplySetToolingValue != "" {
			oldTooling, err := applyset.ParseTooling(currentApplySetToolingValue)
			if err != nil {
				// Hypothetically, we could adopt the ApplySet if the tooling is invalid,
				// but it's safer to error and inform the user.
				return false, fmt.Errorf("%w: remove the %s annotation to allow adoption", err, metadata.ApplySetToolingAnnotation)
			}
			if oldTooling.Name != metadata.ApplySetToolingName {
				// Some other tooling previously claimed this RSync as an ApplySet parent.
				// According to the ApplySet KEP, we MUST error if the tooling name does not match.
				return false, fmt.Errorf("%s applyset owned by %s: remove the %s annotation to allow adoption",
					r.syncGVK.Kind, currentApplySetToolingValue, metadata.ApplySetToolingAnnotation)
			}
			// Else, if the name is ours, we can adopt it and update the version.
			// In the future we may want some version migration behavior,
			// but for now there's only one version, so this case is unlikely.
		}

		if r.logger(ctx).V(5).Enabled() {
			r.logger(ctx).Info("Updating sync metadata: applyset annotation",
				logFieldResourceVersion, rs.GetResourceVersion(),
				"annotationKey", metadata.ApplySetToolingAnnotation,
				"annotationValueOld", currentApplySetToolingValue, "annotationValue", applySetToolingValue)
		}
	}

	if !updateRequired {
		r.logger(ctx).V(5).Info("Sync metadata update skipped: no change")
		return false, nil
	}

	patch := fmt.Sprintf(`{"metadata": {"labels": {%q: %q}, "annotations": {%q: %q}}}`,
		metadata.ApplySetParentIDLabel, applySetID,
		metadata.ApplySetToolingAnnotation, applySetToolingValue)
	err := r.client.Patch(ctx, rs, client.RawPatch(types.MergePatchType, []byte(patch)),
		client.FieldOwner(reconcilermanager.FieldManager))
	if err != nil {
		return true, fmt.Errorf("Sync metadata update failed: %w", err)
	}
	r.logger(ctx).Info("Sync metadata update successful")
	return true, nil
}

func (r *reconcilerBase) requeueRSync(ctx context.Context, obj client.Object, rsRef types.NamespacedName) []reconcile.Request {
	r.logger(ctx).Info(fmt.Sprintf("Changes to %s triggered a reconciliation for the %s (%s).",
		kinds.ObjectSummary(obj), r.syncGVK.Kind, rsRef))
	return []reconcile.Request{
		{NamespacedName: rsRef},
	}
}

func (r *reconcilerBase) requeueAllRSyncs(ctx context.Context, obj client.Object) []reconcile.Request {
	syncMetaList, err := r.listSyncMetadata(ctx)
	if err != nil {
		r.logger(ctx).Error(err, "Failed to list objects",
			logFieldSyncKind, r.syncGVK.Kind)
		return nil
	}
	requests := make([]reconcile.Request, len(syncMetaList.Items))
	for i, syncMeta := range syncMetaList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&syncMeta),
		}
	}
	if len(requests) > 0 {
		r.logger(ctx).Info(fmt.Sprintf("Changes to %s triggered reconciliations for %d %s objects.",
			kinds.ObjectSummary(obj), len(syncMetaList.Items), r.syncGVK.Kind))
	}
	return requests
}

func (r *reconcilerBase) listSyncMetadata(ctx context.Context, opts ...client.ListOption) (*metav1.PartialObjectMetadataList, error) {
	syncMetaList := &metav1.PartialObjectMetadataList{}
	syncMetaList.SetGroupVersionKind(kinds.ListGVKForItemGVK(r.syncGVK))
	if err := r.client.List(ctx, syncMetaList, opts...); err != nil {
		return nil, err
	}
	return syncMetaList, nil
}

// isAnnotationValueTrue returns whether the annotation should be enabled for the
// reconciler of this RSync. This is determined by an annotation that is set on
// the RSync by the reconciler.
func (r *reconcilerBase) isAnnotationValueTrue(ctx context.Context, obj core.Annotated, key string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, ok := obj.GetAnnotations()[key]
	if !ok { // default to disabling the annotation
		return false
	}
	boolVal, err := strconv.ParseBool(val)
	if err != nil {
		// This should never happen, as the annotation should always be set to a
		// valid value by the reconciler. Log the error and return the default value.
		r.logger(ctx).Error(err, "Failed to parse annotation value as boolean: %s: %s", key, val)
		return false
	}
	return boolVal
}
