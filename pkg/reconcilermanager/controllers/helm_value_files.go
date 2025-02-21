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
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// getHelmConfigMapCopyRef returns the NamespacedName of a HelmValuesFile ConfigMap
// that can be mounted to the reconciler deployment pods.
//
// The name of this ConfigMap is deterministic based on the inputs:
//   - nsCMName - the name of the user ConfigMap, in the same namespace as the
//     RepoSync.
//   - rsRef    - the NamespacedName of the RepoSync.
//
// The pattern used is "ns-reconciler-HASH", where the HASH is the hexadecimal
// encoding of the SHA1 hash of a string with the pattern
// "REPOSYNC_NAME:REPOSYNC_NAMESPACE:CONFIGMAP_NAME".
func getHelmConfigMapCopyRef(nsCMName string, rsRef types.NamespacedName) types.NamespacedName {
	// use a hash to prevent the name from being too long
	hash := sha1.Sum([]byte(fmt.Sprintf("%s:%s:%s", rsRef.Name, rsRef.Namespace, nsCMName)))
	return types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", core.NsReconcilerPrefix, hex.EncodeToString(hash[:])),
		Namespace: configsync.ControllerNamespace,
	}
}

// getReconcilerHelmConfigMapRefs returns a list of ValuesFileRefs with the
// associated data key.
func (r *RootSyncReconciler) getReconcilerHelmConfigMapRefs(rs *v1beta1.RootSync) []v1beta1.ValuesFileRef {
	if rs.Spec.Helm == nil {
		return nil
	}
	var cmsCMRefs []v1beta1.ValuesFileRef
	for _, vfRef := range rs.Spec.Helm.ValuesFileRefs {
		cmsCMRefs = append(cmsCMRefs, v1beta1.ValuesFileRef{
			Name:    vfRef.Name,
			DataKey: validate.HelmValuesFileDataKeyOrDefault(vfRef.DataKey),
		})
	}
	return cmsCMRefs
}

// getReconcilerHelmConfigMapRefs returns a list of ValuesFileRefs with the
// names of the HelmValuesFile ConfigMap copies in the config-management-system
// namespace with the associated data key.
func (r *RepoSyncReconciler) getReconcilerHelmConfigMapRefs(rs *v1beta1.RepoSync) []v1beta1.ValuesFileRef {
	if rs.Spec.Helm == nil {
		return nil
	}
	var cmsCMRefs []v1beta1.ValuesFileRef
	for _, vfRef := range rs.Spec.Helm.ValuesFileRefs {
		copyCMRef := getHelmConfigMapCopyRef(vfRef.Name, client.ObjectKeyFromObject(rs))
		cmsCMRefs = append(cmsCMRefs, v1beta1.ValuesFileRef{
			Name:    copyCMRef.Name,
			DataKey: validate.HelmValuesFileDataKeyOrDefault(vfRef.DataKey),
		})
	}
	return cmsCMRefs
}

// upsertHelmConfigMaps creates or updates the helm values file ConfigMaps
// in the config-management-system namespace using existing ConfigMaps in the
// RepoSync namespace.
// Since SourceType or ValuesFileRefs may have changed, we also need to delete
// ConfigMap copies for this RepoSync that are no longer used.
func (r *RepoSyncReconciler) upsertHelmConfigMaps(ctx context.Context, rs *v1beta1.RepoSync, labelMap map[string]string) error {
	rsRef := client.ObjectKeyFromObject(rs)
	var cmNamesToKeep map[string]struct{}
	if rs.Spec.SourceType == configsync.HelmSource && rs.Spec.Helm != nil {
		cmNamesToKeep = make(map[string]struct{}, len(rs.Spec.Helm.ValuesFileRefs))
		for _, vfRef := range rs.Spec.Helm.ValuesFileRefs {
			userCMRef := types.NamespacedName{
				Namespace: rsRef.Namespace,
				Name:      vfRef.Name,
			}
			copyCMRef := getHelmConfigMapCopyRef(userCMRef.Name, rsRef)
			cmNamesToKeep[copyCMRef.Name] = struct{}{}
			userCM, err := r.getUserHelmConfigMap(ctx, userCMRef)
			if err != nil {
				return fmt.Errorf("user config map required for helm values: %s: %w", userCMRef, err)
			}
			if _, err = r.upsertHelmConfigMap(ctx, copyCMRef, rsRef, userCM, labelMap); err != nil {
				return err
			}

		}
	}
	return r.deleteHelmConfigMapCopies(ctx, rsRef, cmNamesToKeep)
}

// getUserHelmConfigMap gets a user managed ConfigMap in the same namespace as
// the RepoSync.
func (r *RepoSyncReconciler) getUserHelmConfigMap(ctx context.Context, userCMRef types.NamespacedName) (*corev1.ConfigMap, error) {
	userCM := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, userCMRef, userCM); err != nil {
		if apierrors.IsNotFound(err) {
			return userCM, fmt.Errorf(
				"config map not found: %s", userCMRef)
		}
		return userCM, fmt.Errorf("config map get failed: %s: %w", userCMRef, err)
	}
	return userCM, nil
}

// upsertHelmConfigMap creates or updates a config map in the
// config-management-system namespace using an existing config map.
func (r *RepoSyncReconciler) upsertHelmConfigMap(ctx context.Context, cmsCMRef, rsRef types.NamespacedName, userCM *corev1.ConfigMap, labelMap map[string]string) (controllerutil.OperationResult, error) {
	cmsConfigMap := &corev1.ConfigMap{}
	cmsConfigMap.Name = cmsCMRef.Name
	cmsConfigMap.Namespace = cmsCMRef.Namespace

	r.Logger(ctx).V(3).Info("Upserting managed object",
		logFieldObjectRef, cmsCMRef.String(),
		logFieldObjectKind, "ConfigMap")
	op, err := CreateOrUpdate(ctx, r.client, cmsConfigMap, func() error {
		// TODO: Eventually remove the annotations and use the labels for list filtering, to optimize cleanup.
		// We can't remove the annotations until v1.16.0 is no longer supported.
		core.AddLabels(cmsConfigMap, labelMap)
		// Add annotations so we can identify where the ConfigMap came from so
		// that we can cleanup properly
		core.AddAnnotations(cmsConfigMap, map[string]string{
			repoSyncNameAnnotationKey:          rsRef.Name,
			repoSyncNamespaceAnnotationKey:     rsRef.Namespace,
			originalConfigMapNameAnnotationKey: userCM.Name,
		})
		// Copy user secret data & type to managed secret
		cmsConfigMap.Data = userCM.Data
		cmsConfigMap.Immutable = userCM.Immutable
		return nil
	})
	if err != nil {
		return op, err
	}
	if op != controllerutil.OperationResultNone {
		r.Logger(ctx).Info("Upserting managed object successful",
			logFieldObjectRef, cmsCMRef.String(),
			logFieldObjectKind, "ConfigMap",
			logFieldOperation, op)
	}
	return op, nil
}
