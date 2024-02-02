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
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/reconcilermanager"

	corev1 "k8s.io/api/core/v1"
)

// updateHydrationControllerImage sets the image of hydration-controller based
// on whether enableShellInRendering is set.
func updateHydrationControllerImage(image string, overrides v1beta1.OverrideSpec) string {
	if overrides.EnableShellInRendering == nil || !*overrides.EnableShellInRendering {
		return strings.ReplaceAll(image, reconcilermanager.HydrationControllerWithShell, reconcilermanager.HydrationController)
	}
	return strings.ReplaceAll(image, reconcilermanager.HydrationController+":", reconcilermanager.HydrationControllerWithShell+":")
}

type hydrationOptions struct {
	sourceType     string
	gitConfig      *v1beta1.Git
	ociConfig      *v1beta1.Oci
	scope          declared.Scope
	reconcilerName string
	pollPeriod     string
}

// hydrationEnvs returns environment variables for the hydration controller.
func hydrationEnvs(opts hydrationOptions) []corev1.EnvVar {
	var result []corev1.EnvVar
	var syncDir string
	switch v1beta1.SourceType(opts.sourceType) {
	case v1beta1.OciSource:
		syncDir = opts.ociConfig.Dir
	case v1beta1.GitSource:
		syncDir = opts.gitConfig.Dir
	case v1beta1.HelmSource:
		syncDir = "."
	}

	result = append(result,
		corev1.EnvVar{
			Name:  reconcilermanager.SourceTypeKey,
			Value: opts.sourceType,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ScopeKey,
			Value: string(opts.scope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerNameKey,
			Value: opts.reconcilerName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.NamespaceNameKey,
			Value: string(opts.scope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncDirKey,
			Value: syncDir,
		},
		// Add Hydration Polling Period.
		corev1.EnvVar{
			Name:  reconcilermanager.HydrationPollingPeriod,
			Value: opts.pollPeriod,
		})
	return result
}

type reconcilerOptions struct {
	clusterName              string
	syncName                 string
	syncGeneration           int64
	reconcilerName           string
	reconcilerScope          declared.Scope
	sourceType               string
	gitConfig                *v1beta1.Git
	ociConfig                *v1beta1.Oci
	helmConfig               *v1beta1.HelmBase
	pollPeriod               string
	statusMode               string
	reconcileTimeout         string
	apiServerTimeout         string
	requiresRendering        bool
	dynamicNSSelectorEnabled bool
}

// reconcilerEnvs returns environment variables for namespace reconciler.
func reconcilerEnvs(opts reconcilerOptions) []corev1.EnvVar {
	var result []corev1.EnvVar
	statusMode := opts.statusMode
	if statusMode == "" {
		statusMode = applier.StatusEnabled
	}
	var syncRepo string
	var syncBranch string
	var syncRevision string
	var syncDir string
	switch v1beta1.SourceType(opts.sourceType) {
	case v1beta1.OciSource:
		syncRepo = opts.ociConfig.Image
		syncDir = opts.ociConfig.Dir
	case v1beta1.HelmSource:
		syncRepo = opts.helmConfig.Repo
		syncDir = opts.helmConfig.Chart
		if opts.helmConfig.Version != "" {
			syncRevision = opts.helmConfig.Version
		} else {
			syncRevision = "latest"
		}
	case v1beta1.GitSource:
		syncRepo = opts.gitConfig.Repo
		syncDir = opts.gitConfig.Dir
		if opts.gitConfig.Branch != "" {
			syncBranch = opts.gitConfig.Branch
		} else {
			syncBranch = "master"
		}
		if opts.gitConfig.Revision != "" {
			syncRevision = opts.gitConfig.Revision
		} else {
			syncRevision = "HEAD"
		}
	}

	result = append(result,
		corev1.EnvVar{
			Name:  reconcilermanager.ClusterNameKey,
			Value: opts.clusterName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ScopeKey,
			Value: string(opts.reconcilerScope),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncNameKey,
			Value: opts.syncName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.SyncGenerationKey,
			Value: fmt.Sprint(opts.syncGeneration),
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerNameKey,
			Value: opts.reconcilerName,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.NamespaceNameKey,
			Value: string(opts.reconcilerScope),
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
			Value: opts.sourceType,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.StatusMode,
			Value: statusMode,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcileTimeout,
			Value: opts.reconcileTimeout,
		},
		// Add Filesystem Polling Period.
		corev1.EnvVar{
			Name:  reconcilermanager.ReconcilerPollingPeriod,
			Value: opts.pollPeriod,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.APIServerTimeout,
			Value: opts.apiServerTimeout,
		},
		corev1.EnvVar{
			Name:  reconcilermanager.RenderingEnabled,
			Value: strconv.FormatBool(opts.requiresRendering),
		},
	)

	if opts.dynamicNSSelectorEnabled {
		result = append(result,
			corev1.EnvVar{
				Name:  reconcilermanager.DynamicNSSelectorEnabled,
				Value: strconv.FormatBool(opts.dynamicNSSelectorEnabled),
			},
		)
	}

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

// namespaceStrategyEnv returns the environment variable for NAMESPACE_STRATEGY in the reconciler container.
func namespaceStrategyEnv(strategy configsync.NamespaceStrategy) corev1.EnvVar {
	if strategy == "" {
		strategy = configsync.NamespaceStrategyImplicit
	}
	return corev1.EnvVar{
		Name:  reconcilermanager.NamespaceStrategy,
		Value: string(strategy),
	}
}

type ociOptions struct {
	image           string
	auth            configsync.AuthType
	period          float64
	caCertSecretRef string
}

// ociSyncEnvs returns the environment variables for the oci-sync container.
func ociSyncEnvs(opts ociOptions) []corev1.EnvVar {
	var result []corev1.EnvVar
	result = append(result, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncImage,
		Value: opts.image,
	}, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncAuth,
		Value: string(opts.auth),
	}, corev1.EnvVar{
		Name:  reconcilermanager.OciSyncWait,
		Value: fmt.Sprintf("%f", opts.period),
	})
	if useCACert(opts.caCertSecretRef) {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.OciCACert,
			Value: fmt.Sprintf("%s/%s", CACertPath, CACertSecretKey),
		})
	}
	return result
}

const (
	// helm-sync container specific environment variables.
	helmSyncName     = "HELM_SYNC_USERNAME"
	helmSyncPassword = "HELM_SYNC_PASSWORD"
)

type helmOptions struct {
	helmBase         *v1beta1.HelmBase
	releaseNamespace string
	deployNamespace  string
	caCertSecretRef  string
}

// helmSyncEnvs returns the environment variables for the helm-sync container.
func helmSyncEnvs(opts helmOptions) []corev1.EnvVar {
	var result []corev1.EnvVar
	helmValues := ""
	if opts.helmBase.Values != nil {
		helmValues = string(opts.helmBase.Values.Raw)
	}
	result = append(result, corev1.EnvVar{
		Name:  reconcilermanager.HelmRepo,
		Value: opts.helmBase.Repo,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmChart,
		Value: opts.helmBase.Chart,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmChartVersion,
		Value: opts.helmBase.Version,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmReleaseName,
		Value: opts.helmBase.ReleaseName,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmReleaseNamespace,
		Value: opts.releaseNamespace,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmDeployNamespace,
		Value: opts.deployNamespace,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmValuesYAML,
		Value: helmValues,
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmIncludeCRDs,
		Value: fmt.Sprint(opts.helmBase.IncludeCRDs),
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmAuthType,
		Value: string(opts.helmBase.Auth),
	}, corev1.EnvVar{
		Name:  reconcilermanager.HelmSyncWait,
		Value: fmt.Sprintf("%f", v1beta1.GetPeriod(opts.helmBase.Period, configsync.DefaultHelmSyncVersionPollingPeriod).Seconds()),
	})
	if useCACert(opts.caCertSecretRef) {
		result = append(result, corev1.EnvVar{
			Name:  reconcilermanager.HelmCACert,
			Value: fmt.Sprintf("%s/%s", CACertPath, CACertSecretKey),
		})
	}
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
// 1. the auth type is `gcpserviceaccount` or `k8sserviceaccount`.
// 2. the cluster is registered in a fleet (the membership object exists).
// 3. the fleet workload identity is enabled (workload_identity_pool and identity_provider are not empty).
func useFWIAuth(authType configsync.AuthType, membership *hubv1.Membership) bool {
	return (authType == configsync.AuthGCPServiceAccount || authType == configsync.AuthK8sServiceAccount) &&
		membership != nil &&
		membership.Spec.IdentityProvider != "" &&
		membership.Spec.WorkloadIdentityPool != ""
}
