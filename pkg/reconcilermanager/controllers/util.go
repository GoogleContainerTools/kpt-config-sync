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
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/reconcilermanager"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// hydrationEnvs returns environment variables for the hydration controller.
func hydrationEnvs(sourceType string, gitConfig *v1beta1.Git, ociConfig *v1beta1.Oci, scope declared.Scope, reconcilerName, pollPeriod string) []corev1.EnvVar {
	var result []corev1.EnvVar
	var syncDir string
	switch v1beta1.SourceType(sourceType) {
	case v1beta1.OciSource:
		syncDir = ociConfig.Dir
	case v1beta1.GitSource:
		syncDir = gitConfig.Dir
	case v1beta1.HelmSource:
		syncDir = "."
	}

	result = append(result,
		corev1.EnvVar{
			Name:  reconcilermanager.SourceTypeKey,
			Value: sourceType,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ScopeKey,
			Value: string(scope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerNameKey,
			Value: reconcilerName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.NamespaceNameKey,
			Value: string(scope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncDirKey,
			Value: syncDir,
		},
		// Add Hydration Polling Period.
		corev1.EnvVar{
			Name:  reconcilermanager.HydrationPollingPeriod,
			Value: pollPeriod,
		})
	return result
}

// reconcilerEnvs returns environment variables for namespace reconciler.
func reconcilerEnvs(clusterName, syncName, reconcilerName string, reconcilerScope declared.Scope, sourceType string, gitConfig *v1beta1.Git, ociConfig *v1beta1.Oci, helmConfig *v1beta1.Helm, pollPeriod, statusMode string, reconcileTimeout string) []corev1.EnvVar {
	var result []corev1.EnvVar
	if statusMode == "" {
		statusMode = applier.StatusEnabled
	}
	var syncRepo string
	var syncBranch string
	var syncRevision string
	var syncDir string
	switch v1beta1.SourceType(sourceType) {
	case v1beta1.OciSource:
		syncRepo = ociConfig.Image
		syncDir = ociConfig.Dir
	case v1beta1.HelmSource:
		syncRepo = helmConfig.Repo
		syncDir = helmConfig.Chart
		if helmConfig.Version != "" {
			syncRevision = helmConfig.Version
		} else {
			syncRevision = "latest"
		}
	case v1beta1.GitSource:
		syncRepo = gitConfig.Repo
		syncDir = gitConfig.Dir
		if gitConfig.Branch != "" {
			syncBranch = gitConfig.Branch
		} else {
			syncBranch = "master"
		}
		if gitConfig.Revision != "" {
			syncRevision = gitConfig.Revision
		} else {
			syncRevision = "HEAD"
		}
	}

	result = append(result,
		corev1.EnvVar{
			Name:  reconcilermanager.ClusterNameKey,
			Value: clusterName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ScopeKey,
			Value: string(reconcilerScope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncNameKey,
			Value: syncName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerNameKey,
			Value: reconcilerName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.NamespaceNameKey,
			Value: string(reconcilerScope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncDirKey,
			Value: syncDir,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SourceRepoKey,
			Value: syncRepo,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SourceTypeKey,
			Value: sourceType,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.StatusMode,
			Value: statusMode,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcileTimeout,
			Value: reconcileTimeout,
		},
		// Add Filesystem Polling Period.
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerPollingPeriod,
			Value: pollPeriod,
		})

	if syncBranch != "" {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.SourceBranchKey,
			Value: syncBranch,
		})
	}
	if syncRevision != "" {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.SourceRevKey,
			Value: syncRevision,
		})
	}
	return result
}

// sourceFormatEnv returns the environment variable for SOURCE_FORMAT in the reconciler container.
func sourceFormatEnv(format string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  filesystem.SourceFormatKey,
		Value: format,
	}
}

// ociSyncEnvs returns the environment variables for the oci-sync container.
func ociSyncEnvs(image string, auth configsync.AuthType, period float64) []corev1.EnvVar {
	var result []corev1.EnvVar
	result = append(result, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncImage,
		Value: image,
	}, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncAuth,
		Value: string(auth),
	}, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncWait,
		Value: fmt.Sprintf("%f", period),
	})
	return result
}

const (
	// helm-sync container specific environment variables.
	helmSyncName     = "HELM_SYNC_USERNAME"
	helmSyncPassword = "HELM_SYNC_PASSWORD"
)

// helmSyncEnvs returns the environment variables for the oci-sync container.
func helmSyncEnvs(repo, chart, version, releaseName, namespace string, auth configsync.AuthType, period float64) []corev1.EnvVar {
	var result []corev1.EnvVar
	result = append(result, corev1.EnvVar{
		Name:  reconcilermanager.HelmRepo,
		Value: repo,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmChart,
		Value: chart,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmChartVersion,
		Value: version,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmReleaseName,
		Value: releaseName,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmReleaseNamespace,
		Value: namespace,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmAuthType,
		Value: string(auth),
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmSyncWait,
		Value: fmt.Sprintf("%f", period),
	})
	return result
}

// helmSyncTokenAuthEnv returns environment variables for helm-sync container for 'token' Auth.
func helmSyncTokenAuthEnv(secretRef string) []corev1.EnvVar {
	helmSyncUsername := &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secretRef,
			},
			Key: "username",
		},
	}

	helmSyncPswd := &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secretRef,
			},
			Key: "password",
		},
	}

	return []corev1.EnvVar{
		{
			Name:      helmSyncName,
			ValueFrom: helmSyncUsername,
		},
		{
			Name:      helmSyncPassword,
			ValueFrom: helmSyncPswd,
		},
	}
}

func ownerReference(kind, name string, uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         v1beta1.SchemeGroupVersion.String(),
		Kind:               kind,
		Name:               name,
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
		UID:                uid,
	}
}

// PollingPeriod parses the polling duration from the environment variable.
// If the variable is not present, it returns the default value.
func PollingPeriod(envName string, defaultValue time.Duration) time.Duration {
	val, present := os.LookupEnv(envName)
	if present {
		pollingFreq, err := time.ParseDuration(val)
		if err != nil {
			panic(errors.Wrapf(err, "failed to parse environment variable %q,"+
				"got value: %v, want err: nil", envName, pollingFreq))
		}
		return pollingFreq
	}
	return defaultValue
}

// useFWIAuth returns whether ConfigSync uses fleet workload identity for authentication.
// It is true only when all the following conditions are true:
// 1. the auth type is `gcpserviceaccount`.
// 2. the cluster is registered in a fleet (the membership object exists).
// 3. the fleet workload identity is enabled (workload_identity_pool and identity_provider are not empty).
func useFWIAuth(authType configsync.AuthType, membership *hubv1.Membership) bool {
	return authType == configsync.AuthGCPServiceAccount && membership != nil &&
		membership.Spec.IdentityProvider != "" && membership.Spec.WorkloadIdentityPool != ""
}
