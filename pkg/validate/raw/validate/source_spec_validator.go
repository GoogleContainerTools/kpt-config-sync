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
	"strings"

	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// gcpSASuffix specifies the default suffix used with gcp ServiceAccount email.
// https://cloud.google.com/iam/docs/service-accounts#user-managed
const gcpSASuffix = ".iam.gserviceaccount.com"

// SourceSpec validates the source specification for any obvious problems.
func SourceSpec(sourceType string, git *v1beta1.Git, oci *v1beta1.Oci, rs client.Object) status.Error {
	switch v1beta1.SourceType(sourceType) {
	case v1beta1.GitSource:
		return GitSpec(git, rs)
	case v1beta1.OciSource:
		return OciSpec(oci, rs)
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
	switch git.Auth {
	case configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthGCENode, configsync.AuthToken, configsync.AuthNone:
	case configsync.AuthGCPServiceAccount:
		if git.GCPServiceAccountEmail == "" {
			return MissingGCPSAEmail(rs)
		}
		if !validGCPServiceAccountEmail(git.GCPServiceAccountEmail) {
			return InvalidGCPSAEmail(rs)
		}
	default:
		return InvalidAuthType(rs)
	}

	// Check that proxy isn't unnecessarily declared.
	if git.Proxy != "" && git.Auth != configsync.AuthNone && git.Auth != configsync.AuthCookieFile && git.Auth != configsync.AuthToken {
		return NoOpProxy(rs)
	}

	// Check the secret ref is specified if and only if it is required.
	switch git.Auth {
	case configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount:
		if git.SecretRef.Name != "" {
			return IllegalSecretRef(rs)
		}
	default:
		if git.SecretRef.Name == "" {
			return MissingSecretRef(rs)
		}
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
	switch oci.Auth {
	case configsync.AuthGCENode, configsync.AuthNone:
	case configsync.AuthGCPServiceAccount:
		if oci.GCPServiceAccountEmail == "" {
			return MissingGCPSAEmail(rs)
		}
		if !validGCPServiceAccountEmail(oci.GCPServiceAccountEmail) {
			return InvalidGCPSAEmail(rs)
		}
	default:
		return InvalidOciAuthType(rs)
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
		Sprintf("%ss must specify spec.git when spec.sourceType is %q", kind, v1beta1.GitSource).
		BuildWithResources(o)
}

// MissingGitRepo reports that a RootSync/RepoSync doesn't declare the git repo it is
// supposed to connect to.
func MissingGitRepo(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.repo when spec.sourceType is %q", kind, v1beta1.GitSource).
		BuildWithResources(o)
}

// InvalidAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods.
func InvalidAuthType(o client.Object) status.Error {
	types := []string{configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthGCENode, configsync.AuthToken, configsync.AuthNone, configsync.AuthGCPServiceAccount}
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.git.auth to be one of %s", kind,
			strings.Join(types, ",")).
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

// IllegalSecretRef reports that a RootSync/RepoSync declares an auth mode that doesn't
// allow SecretRefs does declare a SecretRef.
func IllegalSecretRef(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.auth as one of %q, %q, or %q must not specify spec.git.secretRef",
			kind, configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount).
		BuildWithResources(o)
}

// MissingSecretRef reports that a RootSync/RepoSync declares an auth mode that requires
// a SecretRef, but does not do so.
func MissingSecretRef(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.auth as one of %q, %q or %q must also specify spec.git.secretRef",
			kind, configsync.AuthSSH, configsync.AuthCookieFile, configsync.AuthToken).
		BuildWithResources(o)
}

// InvalidGCPSAEmail reports that a RepoSync/RootSync Resource doesn't have the
//  correct gcp service account suffix.
func InvalidGCPSAEmail(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.auth or spec.oci.auth as %q must use suffix <gcp_serviceaccount_name>.[%s]",
			kind, configsync.AuthGCPServiceAccount, gcpSASuffix).
		BuildWithResources(o)
}

// MissingGCPSAEmail reports that a RepoSync/RootSync resource declares an auth
// mode that requires a GCPServiceAccountEmail, but does not do so.
func MissingGCPSAEmail(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which specify spec.git.auth or spec.oci.auth as %q must also specify spec.git.gcpServiceAccountEmail or spec.oci.gcpServiceAccountEmail",
			kind, configsync.AuthGCPServiceAccount).
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
		Sprintf("%ss must specify spec.sourceType to be either %q or %q", kind, v1beta1.GitSource, v1beta1.OciSource).
		BuildWithResources(o)
}

// MissingOciSpec reports that a RootSync/RepoSync doesn't declare the OCI spec
// when spec.sourceType is set to `oci`.
func MissingOciSpec(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci when spec.sourceType is %q", kind, v1beta1.OciSource).
		BuildWithResources(o)
}

// MissingOciImage reports that a RootSync/RepoSync doesn't declare the OCI image it is
// supposed to connect to.
func MissingOciImage(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.image when spec.sourceType is %q", kind, v1beta1.OciSource).
		BuildWithResources(o)
}

// InvalidOciAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods for OCI image.
func InvalidOciAuthType(o client.Object) status.Error {
	types := []string{configsync.AuthGCENode, configsync.AuthGCPServiceAccount, configsync.AuthNone}
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must specify spec.oci.auth to be one of %s", kind,
			strings.Join(types, ",")).
		BuildWithResources(o)
}
