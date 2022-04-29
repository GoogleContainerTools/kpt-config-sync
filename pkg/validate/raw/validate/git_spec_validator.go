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

var (
	authSSH               = configsync.GitSecretSSH
	authCookiefile        = configsync.GitSecretCookieFile
	authGCENode           = configsync.GitSecretGCENode
	authToken             = configsync.GitSecretToken
	authNone              = configsync.GitSecretNone
	authGCPServiceAccount = configsync.GitSecretGCPServiceAccount
)

// gcpSASuffix specifies the default suffix used with gcp ServiceAccount email.
// https://cloud.google.com/iam/docs/service-accounts#user-managed
const gcpSASuffix = ".iam.gserviceaccount.com"

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
	case authSSH, authCookiefile, authGCENode, authToken, authNone:
	case authGCPServiceAccount:
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
	if git.Proxy != "" && git.Auth != authNone && git.Auth != authCookiefile && git.Auth != authToken {
		return NoOpProxy(rs)
	}

	// Check the secret ref is specified if and only if it is required.
	switch git.Auth {
	case authNone, authGCENode, authGCPServiceAccount:
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

// InvalidSyncCode is the code for an invalid declared RootSync/RepoSync.
var InvalidSyncCode = "1061"

var invalidSyncBuilder = status.NewErrorBuilder(InvalidSyncCode)

// MissingGitSpec reports that a RootSync/RepoSync doesn't declare the git spec
// when spec.sourceType is set to `git`.
func MissingGitSpec(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must define spec.git when spec.sourceType is %q", kind, v1beta1.GitSource).
		BuildWithResources(o)
}

// MissingGitRepo reports that a RootSync/RepoSync doesn't declare the git repo it is
// supposed to connect to.
func MissingGitRepo(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must define spec.git.repo", kind).
		BuildWithResources(o)
}

// InvalidAuthType reports that a RootSync/RepoSync doesn't use one of the known auth
// methods.
func InvalidAuthType(o client.Object) status.Error {
	types := []string{authSSH, authCookiefile, authGCENode, authToken, authNone}
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss must declare spec.git.auth to be one of [%s]", kind,
			strings.Join(types, ",")).
		BuildWithResources(o)
}

// NoOpProxy reports that a RootSync/RepoSync declares a proxy, but the declaration would
// do nothing.
func NoOpProxy(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss which declare spec.git.proxy must declare spec.git.auth=%q, %q or %q",
			kind, authNone, authCookiefile, authToken).
		BuildWithResources(o)
}

// IllegalSecretRef reports that a RootSync/RepoSync declares an auth mode that doesn't
// allow SecretRefs does declare a SecretRef.
func IllegalSecretRef(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss declaring spec.git.auth : [%q, %q, %q] must not declare spec.git.secretRef",
			kind, authNone, authGCENode, authGCPServiceAccount).
		BuildWithResources(o)
}

// MissingSecretRef reports that a RootSync/RepoSync declares an auth mode that requires
// a SecretRef, but does not do so.
func MissingSecretRef(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss declaring spec.git.auth = %q or %q or %q must declare spec.git.secretRef",
			kind, authSSH, authCookiefile, authToken).
		BuildWithResources(o)
}

// InvalidGCPSAEmail reports that a RepoSync/RootSync Resource doesn't have the
//  correct gcp service account suffix.
func InvalidGCPSAEmail(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss declaring spec.git.auth = %q must have suffix <gcp_serviceaccount_name>.[%s]",
			kind, authGCPServiceAccount, gcpSASuffix).
		BuildWithResources(o)
}

// MissingGCPSAEmail reports that a RepoSync/RootSync resource declares an auth
// mode that requires a GCPServiceAccountEmail, but does not do so.
func MissingGCPSAEmail(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return invalidSyncBuilder.
		Sprintf("%ss declaring  spec.git.auth = %q must declare spec.git.gcpServiceAccountEmail",
			kind, authGCPServiceAccount).
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
