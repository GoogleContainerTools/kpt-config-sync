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
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
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

// RepoSyncSpec validates the Repo Sync source specification for any obvious problems.
func RepoSyncSpec(sourceType string, git *v1beta1.Git, oci *v1beta1.Oci, helm *v1beta1.HelmRepoSync, rs client.Object) status.Error {
	switch configsync.SourceType(sourceType) {
	case configsync.GitSource:
		return GitSpec(git, rs)
	case configsync.OciSource:
		return OciSpec(oci, rs)
	case configsync.HelmSource:
		return HelmSpec(reposync.GetHelmBase(helm), rs)
	default:
		return InvalidSourceType(rs)
	}
}

// RootSyncSpec validates the Root Sync source specification for any obvious problems.
func RootSyncSpec(sourceType string, git *v1beta1.Git, oci *v1beta1.Oci, helm *v1beta1.HelmRootSync, rs client.Object) status.Error {
	switch configsync.SourceType(sourceType) {
	case configsync.GitSource:
		return GitSpec(git, rs)
	case configsync.OciSource:
		return OciSpec(oci, rs)
	case configsync.HelmSource:
		if err := HelmSpec(rootsync.GetHelmBase(helm), rs); err != nil {
			return err
		}
		if helm.Namespace != "" && helm.DeployNamespace != "" {
			return HelmNSAndDeployNS(rs)
		}
		return nil
	default:
		return InvalidSourceType(rs)
	}
}

// GitSpec validates the git specification for any obvious problems.
func GitSpec(git *v1beta1.Git, rs client.Object) status.Error {
	if git == nil {
		return MissingGitSpec(rs)
	}

	// We can't connect to the git repo if we don't have the URL.
	if git.Repo == "" {
		return MissingGitRepo(rs)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	if !configsync.IsAuthTypeSupported(configsync.GitSource, git.Auth) {
		return InvalidGitAuthType(rs)
	}

	if err := validateGSAEmail(configsync.GitSource, git.Auth, git.GCPServiceAccountEmail, rs); err != nil {
		return err
	}

	if err := validateSecretRef(configsync.GitSource, git.Auth, git.SecretRef, rs); err != nil {
		return err
	}

	// Check that proxy isn't unnecessarily declared.
	if git.Proxy != "" && git.Auth != configsync.AuthNone && git.Auth != configsync.AuthCookieFile && git.Auth != configsync.AuthToken {
		return NoOpProxy(rs)
	}

	return nil
}

// OciSpec validates the OCI specification for any obvious problems.
func OciSpec(oci *v1beta1.Oci, rs client.Object) status.Error {
	if oci == nil {
		return MissingOciSpec(rs)
	}

	// We can't connect to the oci image if we don't have the URL.
	if oci.Image == "" {
		return MissingOciImage(rs)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	if !configsync.IsAuthTypeSupported(configsync.OciSource, oci.Auth) {
		return InvalidOciAuthType(rs)
	}
	return validateGSAEmail(configsync.OciSource, oci.Auth, oci.GCPServiceAccountEmail, rs)
}

// validateGSAEmail checks whether gcpServiceAccountEmail is specified when
// using the gcpserviceaccount auth type.
func validateGSAEmail(sourceType configsync.SourceType, auth configsync.AuthType,
	gsaEmail string, rs client.Object) status.Error {
	// Only the gcpserviceaccount auth type requires GSAEmail
	if auth != configsync.AuthGCPServiceAccount {
		return nil
	}

	if gsaEmail == "" {
		return MissingGCPSAEmail(sourceType, rs)
	}
	if !validGCPServiceAccountEmail(gsaEmail) {
		return InvalidGCPSAEmail(sourceType, rs)
	}
	return nil
}

// AuthRequiresSecret returns true if the given source type and auth type
// requires the secret.
// Below lists the auth types of each source type that require secret:
// - Git: ssh, token, cookiefile
// - Helm: token
// - oci: N/A
func AuthRequiresSecret(sourceType configsync.SourceType, auth configsync.AuthType) bool {
	switch sourceType {
	case configsync.GitSource:
		return auth == configsync.AuthSSH ||
			auth == configsync.AuthToken ||
			auth == configsync.AuthCookieFile
	case configsync.HelmSource:
		return auth == configsync.AuthToken
	}
	return false
}

// validateSecretRef checks whether the secretRef is specified when needed.
func validateSecretRef(sourceType configsync.SourceType, auth configsync.AuthType,
	secretRef *v1beta1.SecretReference, rs client.Object) status.Error {
	if !AuthRequiresSecret(sourceType, auth) {
		return nil
	}

	if secretRef == nil || secretRef.Name == "" {
		return MissingSecretRef(sourceType, rs)
	}
	return nil
}

// HelmSpec validates the Helm specification for any obvious problems.
func HelmSpec(helm *v1beta1.HelmBase, rs client.Object) status.Error {
	if helm == nil {
		return MissingHelmSpec(rs)
	}

	// We can't locate the helm chart if we don't have the URL.
	if helm.Repo == "" {
		return MissingHelmRepo(rs)
	}

	// We can't locate the helm chart if we don't have the chart name.
	if helm.Chart == "" {
		return MissingHelmChart(rs)
	}

	// Ensure auth is a valid value.
	// Note that Auth is a case-sensitive field, so ones with arbitrary capitalization
	// will fail to apply.
	if !configsync.IsAuthTypeSupported(configsync.HelmSource, helm.Auth) {
		return InvalidHelmAuthType(rs)
	}
	if err := validateGSAEmail(configsync.HelmSource, helm.Auth, helm.GCPServiceAccountEmail, rs); err != nil {
		return err
	}
	if err := validateSecretRef(configsync.HelmSource, helm.Auth, helm.SecretRef, rs); err != nil {
		return err
	}

	for _, vf := range helm.ValuesFileRefs {
		if vf.Name == "" {
			return MissingHelmValuesFileRefsName(rs)
		}
	}

	return nil
}

// ValuesFileRefs checks that the ConfigMaps specified by valuesFileRefs exist, are immutable, and have the provided data key.
func ValuesFileRefs(ctx context.Context, cl client.Client, rs client.Object, valuesFileRefs []v1beta1.ValuesFileRef) status.Error {
	for _, vf := range valuesFileRefs {
		objRef := types.NamespacedName{
			Name:      vf.Name,
			Namespace: rs.GetNamespace(),
		}
		var cm corev1.ConfigMap
		if err := cl.Get(ctx, objRef, &cm); err != nil {
			return HelmValuesMissingConfigMap(rs, err)
		}
		if cm.Immutable == nil || !(*cm.Immutable) {
			return HelmValuesConfigMapMustBeImmutable(rs, objRef.Name)
		}
		dataKey := HelmValuesFileDataKeyOrDefault(vf.DataKey)
		if _, found := cm.Data[dataKey]; !found {
			return HelmValuesMissingConfigMapKey(rs, objRef.Name, dataKey)
		}
	}
	return nil
}

// InvalidSyncCode is the code for an invalid declared RootSync/RepoSync.
var InvalidSyncCode = "1061"

var invalidSyncBuilder = status.NewErrorBuilder(InvalidSyncCode)

// MissingGitSpec reports that a RootSync/RepoSync doesn't declare the git spec
// when spec.sourceType is set to `git`.
func MissingGitSpec(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git when spec.sourceType is %q", kind, configsync.GitSource).
		BuildWithResources(o)
}

// MissingGitRepo reports that a RootSync/RepoSync doesn't declare the git repo it is
// supposed to connect to.
func MissingGitRepo(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.repo when spec.sourceType is %q", kind, configsync.GitSource).
		BuildWithResources(o)
}

// InvalidGitAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods.
func InvalidGitAuthType(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.auth to be one of %s", kind, configsync.SupportedAuthTypes(configsync.GitSource)).
		BuildWithResources(o)
}

// NoOpProxy reports that a RootSync/RepoSync declares a proxy, but the declaration would
// do nothing.
func NoOpProxy(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.proxy must also specify spec.git.auth as one of %q, %q or %q",
			kind, configsync.AuthNone, configsync.AuthCookieFile, configsync.AuthToken).
		BuildWithResources(o)
}

// MissingSecretRef reports that a RootSync/RepoSync declares an auth mode that requires
// a SecretRef, but does not do so.
func MissingSecretRef(sourceType configsync.SourceType, o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as one of %q, %q or %q must also specify spec.%s.secretRef",
			kind, sourceType, configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthToken, sourceType).
		BuildWithResources(o)
}

// InvalidGCPSAEmail reports that a RepoSync/RootSync Resource doesn't have the
//
//	correct gcp service account suffix.
func InvalidGCPSAEmail(sourceType configsync.SourceType, o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as %q must use suffix <gcp_serviceaccount_name>.[%s]",
			kind, sourceType, configsync.AuthGCPServiceAccount, gcpSASuffix).
		BuildWithResources(o)
}

// MissingGCPSAEmail reports that a RepoSync/RootSync resource declares an auth
// mode that requires a GCPServiceAccountEmail, but does not do so.
func MissingGCPSAEmail(sourceType configsync.SourceType, o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.%s.auth as %q must also specify spec.%s.gcpServiceAccountEmail",
			kind, sourceType, configsync.AuthGCPServiceAccount, sourceType).
		BuildWithResources(o)
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
func InvalidSourceType(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.sourceType to be one of %q, %q, %q", kind, configsync.GitSource, configsync.OciSource, configsync.HelmSource).
		BuildWithResources(o)
}

// MissingOciSpec reports that a RootSync/RepoSync doesn't declare the OCI spec
// when spec.sourceType is set to `oci`.
func MissingOciSpec(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci when spec.sourceType is %q", kind, configsync.OciSource).
		BuildWithResources(o)
}

// MissingOciImage reports that a RootSync/RepoSync doesn't declare the OCI image it is
// supposed to connect to.
func MissingOciImage(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.image when spec.sourceType is %q", kind, configsync.OciSource).
		BuildWithResources(o)
}

// InvalidOciAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods for OCI image.
func InvalidOciAuthType(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.auth to be one of %s", kind, configsync.SupportedAuthTypes(configsync.OciSource)).
		BuildWithResources(o)
}

// MissingHelmSpec reports that a RootSync/RepoSync doesn't declare the Helm spec
// when spec.sourceType is set to `helm`.
func MissingHelmSpec(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm when spec.sourceType is %q", kind, configsync.HelmSource).
		BuildWithResources(o)
}

// MissingHelmRepo reports that a RootSync/RepoSync doesn't declare the Helm repository it is
// supposed to download chart from.
func MissingHelmRepo(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.repo when spec.sourceType is %q", kind, configsync.HelmSource).
		BuildWithResources(o)
}

// MissingHelmChart reports that a RootSync/RepoSync doesn't declare the Helm chart name it is
// supposed to rendering.
func MissingHelmChart(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.chart when spec.sourceType is %q", kind, configsync.HelmSource).
		BuildWithResources(o)
}

// InvalidHelmAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods for Helm.
func InvalidHelmAuthType(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.auth to be one of %s", kind, configsync.SupportedAuthTypes(configsync.HelmSource)).
		BuildWithResources(o)
}

// HelmNSAndDeployNS reports that a RootSync has both spec.helm.namespace and spec.helm.deployNamespace
// set, even though they are mutually exclusive
func HelmNSAndDeployNS(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify only one of 'spec.helm.namespace' or 'spec.helm.deployNamespace'", kind).
		BuildWithResources(o)
}

// MissingHelmValuesFileRefsName reports that an RSync is missing spec.helm.valuesFileRefs.name
func MissingHelmValuesFileRefsName(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.helm.valuesFileRefs.name", kind).
		BuildWithResources(o)
}

// HelmValuesMissingConfigMap reports that an RSync is referencing a ConfigMap that doesn't exist.
func HelmValuesMissingConfigMap(o client.Object, err error) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: %s", kind, err.Error()).
		BuildWithResources(o)
}

// HelmValuesMissingConfigMapKey reports that an RSync is missing spec.helm.valuesFileRefs.valuesFile
func HelmValuesMissingConfigMapKey(o client.Object, name, key string) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap %q in namespace %q does not have data key %q", kind, name, o.GetNamespace(), key).
		BuildWithResources(o)
}

// HelmValuesConfigMapMustBeImmutable reports that a referenced ConfigMap from RSync spec.helm.valuesFileRefs is
// not immutable.
func HelmValuesConfigMapMustBeImmutable(o client.Object, name string) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap %q in namespace %q is not immutable", kind, name, o.GetNamespace()).
		BuildWithResources(o)
}
