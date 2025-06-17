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

package validate

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// gcpSASuffix specifies the default suffix used with gcp ServiceAccount email.
// https://cloud.google.com/iam/docs/service-accounts#user-managed
const gcpSASuffix = ".iam.gserviceaccount.com"

// HelmValuesFileDefaultDataKey is the default data key to use when
// spec.helm.valuesFileRefs.dataKey is not specified.
const HelmValuesFileDefaultDataKey = "values.yaml"

// HelmValuesFileDataKeyOrDefault returns the key or the default if the key is
// empty.
func HelmValuesFileDataKeyOrDefault(key string) string {
	if len(key) == 0 {
		return HelmValuesFileDefaultDataKey
	}
	return key
}

// RepoSyncSpec validates the RepoSync source specification.
func RepoSyncSpec(spec v1beta1.RepoSyncSpec) status.Error {
	syncKind := configsync.RepoSyncKind
	switch spec.SourceType {
	case configsync.GitSource:
		if err := GitSpec(spec.Git, syncKind); err != nil {
			return err
		}
	case configsync.OciSource:
		if err := OciSpec(spec.Oci, syncKind); err != nil {
			return err
		}
	case configsync.HelmSource:
		if err := RepoSyncHelmSpec(spec.Helm); err != nil {
			return err
		}
	default:
		return InvalidSourceType(syncKind)
	}
	return RepoSyncOverrideSpec(spec.Override)
}

// RootSyncSpec validates the RootSync source specification.
func RootSyncSpec(spec v1beta1.RootSyncSpec) status.Error {
	syncKind := configsync.RootSyncKind
	switch spec.SourceType {
	case configsync.GitSource:
		if err := GitSpec(spec.Git, syncKind); err != nil {
			return err
		}
	case configsync.OciSource:
		if err := OciSpec(spec.Oci, syncKind); err != nil {
			return err
		}
	case configsync.HelmSource:
		if err := RootSyncHelmSpec(spec.Helm); err != nil {
			return err
		}
	default:
		return InvalidSourceType(syncKind)
	}
	return RootSyncOverrideSpec(spec.Override)
}

// GitSpec validates the git specification.
func GitSpec(git *v1beta1.Git, syncKind string) status.Error {
	if git == nil {
		return MissingGitSpec(syncKind)
	}

	// We can't connect to the git repo if we don't have the URL.
	if git.Repo == "" {
		return MissingGitRepo(syncKind)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	switch git.Auth {
	case configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthGCENode, configsync.AuthToken, configsync.AuthNone, configsync.AuthGithubApp:
	case configsync.AuthGCPServiceAccount:
		if git.GCPServiceAccountEmail == "" {
			return MissingGCPSAEmail(configsync.GitSource, syncKind)
		}
		if !validGCPServiceAccountEmail(git.GCPServiceAccountEmail) {
			return InvalidGCPSAEmail(configsync.GitSource, syncKind)
		}
	default:
		return InvalidGitAuthType(syncKind)
	}

	// Check that proxy isn't unnecessarily declared.
	if git.Proxy != "" && git.Auth != configsync.AuthNone && git.Auth != configsync.AuthCookieFile && git.Auth != configsync.AuthToken {
		return NoOpProxy(syncKind)
	}

	// Check the secret ref is specified if and only if it is required.
	switch git.Auth {
	case configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount:
		if git.SecretRef != nil && git.SecretRef.Name != "" {
			return IllegalSecretRef(configsync.GitSource, syncKind)
		}
	default:
		if git.SecretRef == nil || git.SecretRef.Name == "" {
			return MissingSecretRef(configsync.GitSource, syncKind)
		}
	}

	return nil
}

// OciSpec validates the OCI specification.
func OciSpec(oci *v1beta1.Oci, syncKind string) status.Error {
	if oci == nil {
		return MissingOciSpec(syncKind)
	}

	// We can't connect to the oci image if we don't have the URL.
	if oci.Image == "" {
		return MissingOciImage(syncKind)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	switch oci.Auth {
	case configsync.AuthGCENode, configsync.AuthK8sServiceAccount, configsync.AuthNone:
	case configsync.AuthGCPServiceAccount:
		if oci.GCPServiceAccountEmail == "" {
			return MissingGCPSAEmail(configsync.OciSource, syncKind)
		}
		if !validGCPServiceAccountEmail(oci.GCPServiceAccountEmail) {
			return InvalidGCPSAEmail(configsync.OciSource, syncKind)
		}
	default:
		return InvalidOciAuthType(syncKind)
	}
	return nil
}

// RootSyncHelmSpec validates the RootSync Helm specification.
func RootSyncHelmSpec(helm *v1beta1.HelmRootSync) status.Error {
	syncKind := configsync.RootSyncKind
	if err := HelmSpec(rootsync.GetHelmBase(helm), syncKind); err != nil {
		return err
	}
	if helm.Namespace != "" && helm.DeployNamespace != "" {
		return HelmNSAndDeployNS(syncKind)
	}
	return nil
}

// RepoSyncHelmSpec validates the RepoSync Helm specification.
func RepoSyncHelmSpec(helm *v1beta1.HelmRepoSync) status.Error {
	syncKind := configsync.RepoSyncKind
	return HelmSpec(reposync.GetHelmBase(helm), syncKind)
}

// HelmSpec validates the Helm specification.
func HelmSpec(helm *v1beta1.HelmBase, syncKind string) status.Error {
	if helm == nil {
		return MissingHelmSpec(syncKind)
	}

	// We can't locate the helm chart if we don't have the URL.
	if helm.Repo == "" {
		return MissingHelmRepo(syncKind)
	}

	// We can't locate the helm chart if we don't have the chart name.
	if helm.Chart == "" {
		return MissingHelmChart(syncKind)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	switch helm.Auth {
	case configsync.AuthGCENode, configsync.AuthK8sServiceAccount, configsync.AuthNone:
		if helm.SecretRef != nil && helm.SecretRef.Name != "" {
			return IllegalSecretRef(configsync.HelmSource, syncKind)
		}
	case configsync.AuthToken:
		if helm.SecretRef == nil || helm.SecretRef.Name == "" {
			return MissingSecretRef(configsync.HelmSource, syncKind)
		}
	case configsync.AuthGCPServiceAccount:
		if helm.SecretRef != nil && helm.SecretRef.Name != "" {
			return IllegalSecretRef(configsync.HelmSource, syncKind)
		}
		if helm.GCPServiceAccountEmail == "" {
			return MissingGCPSAEmail(configsync.HelmSource, syncKind)
		}
		if !validGCPServiceAccountEmail(helm.GCPServiceAccountEmail) {
			return InvalidGCPSAEmail(configsync.HelmSource, syncKind)
		}
	default:
		return InvalidHelmAuthType(syncKind)
	}

	for _, vf := range helm.ValuesFileRefs {
		if vf.Name == "" {
			return MissingHelmValuesFileRefsName(syncKind)
		}
	}

	return nil
}

// ValuesFileRefs checks that the ConfigMaps specified by valuesFileRefs exist, are immutable, and have the provided data key.
func ValuesFileRefs(ctx context.Context, cl client.Client, syncKind, syncNamespace string, valuesFileRefs []v1beta1.ValuesFileRef) status.Error {
	for _, vf := range valuesFileRefs {
		objRef := types.NamespacedName{
			Name:      vf.Name,
			Namespace: syncNamespace,
		}
		var cm corev1.ConfigMap
		if err := cl.Get(ctx, objRef, &cm); err != nil {
			return HelmValuesMissingConfigMap(syncKind, err)
		}
		if cm.Immutable == nil || !(*cm.Immutable) {
			return HelmValuesConfigMapMustBeImmutable(syncKind, objRef.Name, objRef.Namespace)
		}
		dataKey := HelmValuesFileDataKeyOrDefault(vf.DataKey)
		if _, found := cm.Data[dataKey]; !found {
			return HelmValuesMissingConfigMapKey(syncKind, objRef.Name, objRef.Namespace, dataKey)
		}
	}
	return nil
}

// RootSyncOverrideSpec validates the RootSync Override specification.
func RootSyncOverrideSpec(override *v1beta1.RootSyncOverrideSpec) status.Error {
	if override == nil {
		return nil
	}
	syncKind := configsync.RootSyncKind
	for _, roleRef := range override.RoleRefs {
		if roleRef.Kind == "Role" && roleRef.Namespace == "" {
			return OverrideRoleRefNamespace(syncKind)
		}
	}
	return OverrideSpec(&override.OverrideSpec, syncKind)
}

// RepoSyncOverrideSpec validates the RepoSync Override specification.
func RepoSyncOverrideSpec(override *v1beta1.RepoSyncOverrideSpec) status.Error {
	if override == nil {
		return nil
	}
	syncKind := configsync.RepoSyncKind
	return OverrideSpec(&override.OverrideSpec, syncKind)
}

// OverrideSpec validates the common Override specification.
func OverrideSpec(override *v1beta1.OverrideSpec, syncKind string) status.Error {
	if override == nil {
		return nil
	}
	for _, containerResources := range override.Resources {
		if containerResources.CPURequest.Sign() < 0 {
			return OverrideResourceQuantityNegative("cpuRequest", syncKind)
		}
		if containerResources.CPULimit.Sign() < 0 {
			return OverrideResourceQuantityNegative("cpuLimit", syncKind)
		}
		if containerResources.MemoryRequest.Sign() < 0 {
			return OverrideResourceQuantityNegative("memoryRequest", syncKind)
		}
		if containerResources.MemoryLimit.Sign() < 0 {
			return OverrideResourceQuantityNegative("memoryLimit", syncKind)
		}
	}
	return nil
}

// ReconcilerName validates the reconciler name.
func ReconcilerName(reconcilerName string) status.Error {
	if errs := validation.IsDNS1123Subdomain(reconcilerName); errs != nil {
		return InvalidReconcilerName(reconcilerName, strings.Join(errs, ", "))
	}
	return nil
}

// RootSyncName validates the RootSync name length.
func RootSyncName(rs *v1beta1.RootSync) status.Error {
	nameLength := len(rs.Name)
	if nameLength > reconcilermanager.MaxRootSyncNameLength {
		return RootSyncNameLengthExceeded(nameLength)
	}
	return nil
}

// RootSyncMetadata validates name, namespace, and annotations.
func RootSyncMetadata(rs *v1beta1.RootSync) status.Error {
	if rs.Namespace != configsync.ControllerNamespace {
		return InvalidRootSyncNamespace(rs.Namespace)
	}

	if err := RootSyncName(rs); err != nil {
		return err
	}
	return DeletionPropagationAnnotation(rs, "RootSync")
}

// RepoSyncName validates RepoSync NN length.
func RepoSyncName(rs *v1beta1.RepoSync) status.Error {
	nnLength := len(rs.Name) + len(rs.Namespace)
	if nnLength > reconcilermanager.MaxRepoSyncNNLength {
		return RepoSyncNNLengthExceeded(nnLength)
	}
	return nil
}

// RepoSyncMetadata validates name, namespace, and annotations.
func RepoSyncMetadata(rs *v1beta1.RepoSync) status.Error {
	if rs.Namespace == configsync.ControllerNamespace {
		return InvalidRepoSyncNamespace()
	}

	if err := RepoSyncName(rs); err != nil {
		return err
	}

	return DeletionPropagationAnnotation(rs, "RepoSync")
}

// InvalidSyncCode is the code for an invalid declared RootSync/RepoSync.
var InvalidSyncCode = "1061"

var invalidSyncBuilder = status.NewErrorBuilder(InvalidSyncCode)

// InvalidSecretName reports that a secret name is invalid.
func InvalidSecretName(secretName, reasons string) status.Error {
	return invalidSyncBuilder.
		Sprintf("The managed secret name %q is invalid: %s. To fix it, update '.spec.git.secretRef.name'", secretName, reasons).
		Build()
}

// MissingSecret reports that a secret was not found.
func MissingSecret(namespaceSecretName string) status.Error {
	return invalidSyncBuilder.
		Sprintf("Secret %s not found: create one to allow client authentication", namespaceSecretName).
		Build()
}

// InvalidSecretAuthType reports that the secret auth type is invalid.
func InvalidSecretAuthType(auth configsync.AuthType) status.Error {
	return invalidSyncBuilder.
		Sprintf("git secretType is set to unsupported value: %q", auth).
		Build()
}

// MissingGithubAppIDInSecret reports that a GithubApp auth ID is missing in a secret.
func MissingGithubAppIDInSecret(secretName string) status.Error {
	return invalidSyncBuilder.
		Sprintf("git secretType was set to %q but one of (github-app-application-id, github-app-client-id) is not present in %v secret",
			configsync.AuthGithubApp, secretName).
		Build()
}

// AmbiguousGithubAppIDInSecret reports that the GithubApp auth ID is ambiguous in a secret.
func AmbiguousGithubAppIDInSecret(secretName string) status.Error {
	return invalidSyncBuilder.
		Sprintf("git secretType was set to %q but more than one of (github-app-application-id, github-app-client-id) is present in %v secret",
			configsync.AuthGithubApp, secretName).
		Build()
}

// MissingKeyInAuthSecret reports that a key is missing in an auth secret.
func MissingKeyInAuthSecret(authType configsync.AuthType, key, secretName string) status.Error {
	return invalidSyncBuilder.
		Sprintf("git secretType was set as %q but %q key is not present in %q secret", authType, key, secretName).
		Build()
}

// MissingKeyInCACertSecret reports that a key is missing in a CA cert secret.
func MissingKeyInCACertSecret(caCertSecretKey, caCertSecretRefName string) status.Error {
	return invalidSyncBuilder.
		Sprintf("caCertSecretRef was set, but %q key is not present in %q secret", caCertSecretKey, caCertSecretRefName).
		Build()
}

// MissingGitSpec reports that a RootSync/RepoSync doesn't declare the git spec
// when spec.sourceType is set to `git`.
func MissingGitSpec(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git when spec.sourceType is %q", syncKind, configsync.GitSource).
		Build()
}

// MissingGitRepo reports that a RootSync/RepoSync doesn't declare the git repo it is
// supposed to connect to.
func MissingGitRepo(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.repo when spec.sourceType is %q", syncKind, configsync.GitSource).
		Build()
}

// InvalidGitAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods.
func InvalidGitAuthType(syncKind string) status.Error {
	types := []string{string(configsync.AuthSSH), string(configsync.AuthCookieFile), string(configsync.AuthGCENode), string(configsync.AuthToken), string(configsync.AuthNone), string(configsync.AuthGCPServiceAccount), string(configsync.AuthGithubApp)}
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.auth to be one of %s", syncKind,
			strings.Join(types, ",")).
		Build()
}

// NoOpProxy reports that a RootSync/RepoSync declares a proxy, but the declaration would
// do nothing.
func NoOpProxy(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.proxy must also specify spec.git.auth as one of %q, %q or %q",
			syncKind, configsync.AuthNone, configsync.AuthCookieFile, configsync.AuthToken).
		Build()
}

// IllegalSecretRef reports that a RootSync/RepoSync declares an auth mode that doesn't
// allow SecretRefs does declare a SecretRef.
func IllegalSecretRef(sourceType configsync.SourceType, syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as one of %q, %q, or %q must not specify spec.%s.secretRef",
			syncKind, sourceType, configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount, sourceType).
		Build()
}

// MissingSecretRef reports that a RootSync/RepoSync declares an auth mode that requires
// a SecretRef, but does not do so.
func MissingSecretRef(sourceType configsync.SourceType, syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as one of %q, %q, %q, or %q must also specify spec.%s.secretRef",
			syncKind, sourceType, configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthGithubApp, configsync.AuthToken, sourceType).
		Build()
}

// InvalidGCPSAEmail reports that a RepoSync/RootSync Resource doesn't have the
//
//	correct gcp service account suffix.
func InvalidGCPSAEmail(sourceType configsync.SourceType, syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as %q must use suffix <gcp_serviceaccount_name>.[%s]",
			syncKind, sourceType, configsync.AuthGCPServiceAccount, gcpSASuffix).
		Build()
}

// MissingGCPSAEmail reports that a RepoSync/RootSync resource declares an auth
// mode that requires a GCPServiceAccountEmail, but does not do so.
func MissingGCPSAEmail(sourceType configsync.SourceType, syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as %q must also specify spec.%s.gcpServiceAccountEmail",
			syncKind, sourceType, configsync.AuthGCPServiceAccount, sourceType).
		Build()
}

// validGCPServiceAccountEmail verifies whether GCP SA email has correct
// prefix and suffix format.
func validGCPServiceAccountEmail(email string) bool {
	if strings.Contains(email, "@") {
		s := strings.Split(email, "@")
		if len(s) == 2 {
			prefix := s[0]
			// Service account name must be between 6 and 30 characters (inclusive),
			if len(prefix) < 6 || len(prefix) > 30 {
				return false
			}
			return strings.HasSuffix(s[1], gcpSASuffix)
		}
	}
	return false
}

// InvalidSourceType reports that a RootSync/RepoSync doesn't use one of the
// supported source types.
func InvalidSourceType(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.sourceType to be one of %q, %q, %q", syncKind, configsync.GitSource, configsync.OciSource, configsync.HelmSource).
		Build()
}

// RepoSyncNNLengthExceeded reports that the RootSync name and namespace exceeds the max length
func RepoSyncNNLengthExceeded(length int) status.Error {
	return invalidSyncBuilder.
		Sprintf("maximum combined length of RepoSync name and namespace is %d, but found %d", reconcilermanager.MaxRepoSyncNNLength, length).
		Build()
}

// RootSyncNameLengthExceeded reports that the RootSync name exceeds the max length
func RootSyncNameLengthExceeded(length int) status.Error {
	return invalidSyncBuilder.
		Sprintf("maximum length of RootSync name is %d, but found %d", reconcilermanager.MaxRootSyncNameLength, length).
		Build()
}

// InvalidRepoSyncNamespace reports that a RepoSync has an invalid namespace
func InvalidRepoSyncNamespace() status.Error {
	return invalidSyncBuilder.
		Sprintf("RepoSync objects are not allowed in the %s namespace", configsync.ControllerNamespace).
		Build()
}

// InvalidRootSyncNamespace reports that a RootSync has an invalid namespace
func InvalidRootSyncNamespace(namespace string) status.Error {
	return invalidSyncBuilder.
		Sprintf("RootSync objects are only allowed in the %s namespace, not in %s", configsync.ControllerNamespace, namespace).
		Build()
}

// InvalidReconcilerName reports that the reconciler name is invalid
func InvalidReconcilerName(reconcilerName, reasons string) status.Error {
	return invalidSyncBuilder.
		Sprintf("Invalid reconciler name %q: %s", reconcilerName, reasons).
		Build()
}

// MissingOciSpec reports that a RootSync/RepoSync doesn't declare the OCI spec
// when spec.sourceType is set to `oci`.
func MissingOciSpec(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci when spec.sourceType is %q", syncKind, configsync.OciSource).
		Build()
}

// MissingOciImage reports that a RootSync/RepoSync doesn't declare the OCI image it is
// supposed to connect to.
func MissingOciImage(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.image when spec.sourceType is %q", syncKind, configsync.OciSource).
		Build()
}

// InvalidOciAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods for OCI image.
func InvalidOciAuthType(syncKind string) status.Error {
	types := []string{string(configsync.AuthGCENode), string(configsync.AuthGCPServiceAccount), string(configsync.AuthK8sServiceAccount), string(configsync.AuthNone)}
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.auth to be one of %s", syncKind,
			strings.Join(types, ",")).
		Build()
}

// MissingHelmSpec reports that a RootSync/RepoSync doesn't declare the Helm spec
// when spec.sourceType is set to `helm`.
func MissingHelmSpec(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm when spec.sourceType is %q", syncKind, configsync.HelmSource).
		Build()
}

// MissingHelmRepo reports that a RootSync/RepoSync doesn't declare the Helm repository it is
// supposed to download chart from.
func MissingHelmRepo(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.repo when spec.sourceType is %q", syncKind, configsync.HelmSource).
		Build()
}

// MissingHelmChart reports that a RootSync/RepoSync doesn't declare the Helm chart name it is
// supposed to rendering.
func MissingHelmChart(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.chart when spec.sourceType is %q", syncKind, configsync.HelmSource).
		Build()
}

// InvalidHelmAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods for Helm.
func InvalidHelmAuthType(syncKind string) status.Error {
	types := []string{string(configsync.AuthGCENode), string(configsync.AuthGCPServiceAccount), string(configsync.AuthK8sServiceAccount), string(configsync.AuthNone), string(configsync.AuthToken)}
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.auth to be one of %s", syncKind,
			strings.Join(types, ",")).
		Build()
}

// HelmNSAndDeployNS reports that a RootSync has both spec.helm.namespace and spec.helm.deployNamespace
// set, even though they are mutually exclusive
func HelmNSAndDeployNS(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify only one of 'spec.helm.namespace' or 'spec.helm.deployNamespace'", syncKind).
		Build()
}

// MissingHelmValuesFileRefsName reports that an RSync is missing spec.helm.valuesFileRefs.name
func MissingHelmValuesFileRefsName(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.valuesFileRefs.name", syncKind).
		Build()
}

// HelmValuesMissingConfigMap reports that an RSync is referencing a ConfigMap that doesn't exist.
func HelmValuesMissingConfigMap(syncKind string, err error) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: %s", syncKind, err.Error()).
		Build()
}

// HelmValuesMissingConfigMapKey reports that an RSync is missing spec.helm.valuesFileRefs.valuesFile
func HelmValuesMissingConfigMapKey(syncKind, cmName, cmNamespace, dataKey string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap %q in namespace %q does not have data key %q", syncKind, cmName, cmNamespace, dataKey).
		Build()
}

// HelmValuesConfigMapMustBeImmutable reports that a referenced ConfigMap from RSync spec.helm.valuesFileRefs is
// not immutable.
func HelmValuesConfigMapMustBeImmutable(syncKind, cmName, cmNamespace string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap %q in namespace %q is not immutable", syncKind, cmName, cmNamespace).
		Build()
}

// OverrideRoleRefNamespace reports that a RootSync needs
// `spec.override.roleRefs.namespace` when  `spec.override.roleRefs.kind` is
// "Role".
func OverrideRoleRefNamespace(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss must specify 'spec.override.roleRefs.namespace' for role references with kind Role", syncKind).
		Build()
}

// OverrideResourceQuantityNegative reports that a RootSync needs
// `spec.override.roleRefs.namespace` when  `spec.override.roleRefs.kind` is
// "Role".
func OverrideResourceQuantityNegative(fieldName, syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%s field 'spec.override.resources.%s' must not be negative", syncKind, fieldName).
		Build()
}
