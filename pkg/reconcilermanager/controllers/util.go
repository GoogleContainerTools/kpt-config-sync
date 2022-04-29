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
	"os"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/reconcilermanager"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// hydrationData returns configmap data for the hydration controller.
func hydrationEnvs(gitConfig *v1beta1.Git, scope declared.Scope, reconcilerName, pollPeriod string) []corev1.EnvVar {
	var result []corev1.EnvVar
	result = append(result,
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
			Value: gitConfig.Dir,
		},
		// Add Hydration Polling Period.
		corev1.EnvVar{
			Name:  reconcilermanager.HydrationPollingPeriod,
			Value: pollPeriod,
		})
	return result
}

// reconcilerData returns configmap data for namespace reconciler.
func reconcilerEnvs(clusterName, syncName, reconcilerName string, reconcilerScope declared.Scope, gitConfig *v1beta1.Git, pollPeriod, statusMode string, reconcileTimeout string) []corev1.EnvVar {
	var result []corev1.EnvVar
	if statusMode == "" {
		statusMode = applier.StatusEnabled
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
			Name:  reconcilermanager.PolicyDirKey,
			Value: gitConfig.Dir,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.GitRepoKey,
			Value: gitConfig.Repo,
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

	if gitConfig.Branch != "" {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.GitBranchKey,
			Value: gitConfig.Branch,
		})
	} else {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.GitBranchKey,
			Value: "master",
		})
	}
	if gitConfig.Revision != "" {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.GitRevKey,
			Value: gitConfig.Revision,
		})
	} else {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.GitRevKey,
			Value: "HEAD",
		})
	}
	return result
}

// sourceFormatData returns configmap for reconciler.
func sourceFormatEnv(format string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  filesystem.SourceFormatKey,
		Value: format,
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
