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
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"
)

const (
	// git-sync container specific environment variables.

	// gitSyncUsername represents the environment variable key for specifying the username to use for git auth.
	gitSyncUsername = "GITSYNC_USERNAME"
	// gitSyncPassword represents the environment variable key for specifying the password to use for git auth.
	gitSyncPassword = "GITSYNC_PASSWORD"
	// gitSyncHTTPSProxy represents the environment variable key for setting `HTTPS_PROXY` in git-sync.
	gitSyncHTTPSProxy = "HTTPS_PROXY"
	// GitSyncRepo represents the environment variable key for specifying the Git repository to sync.
	GitSyncRepo = "GITSYNC_REPO"
	// gitSyncRef represents the environment variable key for specifying the Git revision to sync.
	gitSyncRef = "GITSYNC_REF"
	// GitSyncDepth represents the environment variable key for setting the depth of the Git clone, truncating history to a specific number of commits.
	GitSyncDepth = "GITSYNC_DEPTH"
	// gitSyncPeriod represents the environment variable key for specifying the sync interval duration.
	gitSyncPeriod = "GITSYNC_PERIOD"

	// gitSyncSSH represents the environment variable key for specifying the SSH key to use.
	gitSyncSSH = "GITSYNC_SSH"
	// gitSyncAskpassURL represents the environment variable key for the URL used to query git credentials.
	gitSyncAskpassURL = "GITSYNC_ASKPASS_URL"

	// gitSyncCookieFile represents the environment variable key for specifying the use of a git cookiefile.
	gitSyncCookieFile = "GITSYNC_COOKIE_FILE"

	// GitSSLCAInfo represents the environment variable key for SSL certificates.
	GitSSLCAInfo = "GIT_SSL_CAINFO"

	// GitSyncKnownHosts represents the environment variable key for GIT_KNOWN_HOSTS.
	GitSyncKnownHosts = "GITSYNC_SSH_KNOWN_HOSTS"
	// GitSSLNoVerify represents the environment variable key for GIT_SSL_NO_VERIFY.
	GitSSLNoVerify = "GIT_SSL_NO_VERIFY"

	// GithubAppBaseURL is an optional parameter to override the GitHub api endpoint
	GithubAppBaseURL = "GITSYNC_GITHUB_BASE_URL"
	// GithubAppPrivateKey is the private key used for GitHub App authentication
	GithubAppPrivateKey = "GITSYNC_GITHUB_APP_PRIVATE_KEY"
	// GithubAppClientID is the client id used for GitHub App authentication
	GithubAppClientID = "GITSYNC_GITHUB_APP_CLIENT_ID"
	// GithubAppApplicationID is the app id used for GitHub App authentication
	GithubAppApplicationID = "GITSYNC_GITHUB_APP_APPLICATION_ID"
	// GithubAppInstallationID is the installation id used for GitHub App authentication
	GithubAppInstallationID = "GITSYNC_GITHUB_APP_INSTALLATION_ID"

	// DefaultSyncRev is the default git revision.
	DefaultSyncRev = "HEAD"
	// DefaultSyncBranch is the default git branch.
	DefaultSyncBranch = "master"
	// DefaultSyncDir is the default sync directory.
	DefaultSyncDir = "."
	// DefaultSyncWaitSecs is the default wait seconds.
	DefaultSyncWaitSecs = 15
	// SyncDepthNoRev is the default git depth if syncing with default sync revision (`HEAD`).
	SyncDepthNoRev = "1"
	// SyncDepthRev is the default git depth if syncing with a specific sync revision (tag or hash).
	SyncDepthRev = "500"
	// KnownHostsKey is the key for known_hosts information
	KnownHostsKey = "known_hosts"
)

var gceNodeAskpassURL = fmt.Sprintf("http://localhost:%v/git_askpass", gceNodeAskpassPort)

// githubAppFromSecret maps the non-sensitive values from the githubapp Secret
// to an internal representation.
func githubAppFromSecret(secret *corev1.Secret) githubAppSpec {
	githubApp := githubAppSpec{}
	if clientID, ok := secret.Data[GitSecretGithubAppClientID]; ok {
		githubApp.clientID = string(clientID)
	}
	if appID, ok := secret.Data[GitSecretGithubAppApplicationID]; ok {
		githubApp.appID = string(appID)
	}
	if installationID, ok := secret.Data[GitSecretGithubAppInstallationID]; ok {
		githubApp.installationID = string(installationID)
	}
	if baseURL, ok := secret.Data[GitSecretGithubAppBaseURL]; ok {
		githubApp.baseURL = string(baseURL)
	}
	return githubApp
}

// githubAppSpec stores the values from the githubapp auth secret which are
// mapped directly to env vars. These are all non-sensitive values.
type githubAppSpec struct {
	clientID       string
	appID          string
	installationID string
	baseURL        string
}

// GitSyncEnvVars maps the github app configuration to git-sync env vars
func (g githubAppSpec) GitSyncEnvVars(secretRef string) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name: GithubAppPrivateKey,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretRef,
					},
					Key: GitSecretGithubAppPrivateKey,
				},
			},
		},
	}
	if g.clientID != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  GithubAppClientID,
			Value: g.clientID,
		})
	}
	if g.appID != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  GithubAppApplicationID,
			Value: g.appID,
		})
	}
	if g.installationID != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  GithubAppInstallationID,
			Value: g.installationID,
		})
	}
	if g.baseURL != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  GithubAppBaseURL,
			Value: g.baseURL,
		})
	}
	return envVars
}

type options struct {
	// ref is the git revision being synced.
	ref string
	// branch is the git branch being synced.
	branch string
	// repo is the git repo being synced.
	repo string
	// secretType used to connect to the repo.
	secretType configsync.AuthType
	// proxy used to connect to the repo.
	proxy string
	// period is the time duration between consecutive syncs.
	period time.Duration
	// depth is the number of git commits to sync.
	depth *int64
	// noSSLVerify specifies whether to skip the SSL certificate verification in Git.
	noSSLVerify bool
	// caCertSecretRef specifies the name of a secret containing a CA certificate
	caCertSecretRef string
	// knownHost specifies whether known_hosts configuration is included
	knownHost bool
}

// gitSyncTokenAuthEnv returns environment variables for git-sync container for 'token' Auth.
func gitSyncTokenAuthEnv(secretRef string) []corev1.EnvVar {
	username := &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secretRef,
			},
			Key: "username",
		},
	}

	passwd := &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secretRef,
			},
			Key: "token",
		},
	}

	return []corev1.EnvVar{
		{
			Name:      gitSyncUsername,
			ValueFrom: username,
		},
		{
			Name:      gitSyncPassword,
			ValueFrom: passwd,
		},
	}
}

// gitSyncHttpsProxyEnv returns environment variables for git-sync container for https_proxy env.
func gitSyncHTTPSProxyEnv(secretRef string, keys map[string]bool) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	if keys["https_proxy"] {
		httpsProxy := &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretRef,
				},
				Key: "https_proxy",
			},
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:      gitSyncHTTPSProxy,
			ValueFrom: httpsProxy,
		})
	}
	return envVars
}

func authTypeToken(secret configsync.AuthType) bool {
	return configsync.AuthToken == secret
}

func useCACert(caCertSecretRef string) bool {
	return caCertSecretRef != ""
}

func gitSyncEnvs(_ context.Context, opts options) ([]corev1.EnvVar, error) {
	var result []corev1.EnvVar
	result = append(result, corev1.EnvVar{
		Name:  GitSyncRepo,
		Value: opts.repo,
	})

	// GitSyncKnownHosts must default to false for backwards compatibility
	if opts.knownHost {
		result = append(result, corev1.EnvVar{
			Name:  GitSyncKnownHosts,
			Value: "true",
		})
	} else {
		result = append(result, corev1.EnvVar{
			Name:  GitSyncKnownHosts,
			Value: "false",
		})
	}
	if opts.noSSLVerify {
		result = append(result, corev1.EnvVar{
			Name:  GitSSLNoVerify,
			Value: "true",
		})
	}
	if useCACert(opts.caCertSecretRef) {
		result = append(result, corev1.EnvVar{
			Name:  GitSSLCAInfo,
			Value: fmt.Sprintf("%s/%s", CACertPath, CACertSecretKey),
		})
	}
	if opts.depth != nil && *opts.depth >= 0 {
		// git-sync would do a shallow clone if *opts.depth > 0;
		// git-sync would do a full clone if *opts.depth == 0.
		result = append(result, corev1.EnvVar{
			Name:  GitSyncDepth,
			Value: strconv.FormatInt(*opts.depth, 10),
		})
	} else {
		// git-sync would do a shallow clone.
		//
		// If syncRev is set, git-sync checks out the source repo at master and then resets to
		// the specified rev. This means that the rev has to be in the pulled history and thus
		// will fail if rev is earlier than the configured depth.
		// However, if history is too large git-sync will OOM when it tries to pull all of it.
		// Try to set a happy medium here -- if syncRev is set, pull 500 commits from master;
		// if it isn't, just the latest commit will do and will save memory.
		// See b/175088702 and b/158988143
		if opts.ref == "" || opts.ref == DefaultSyncRev {
			result = append(result, corev1.EnvVar{
				Name:  GitSyncDepth,
				Value: SyncDepthNoRev,
			})
		} else {
			result = append(result, corev1.EnvVar{
				Name:  GitSyncDepth,
				Value: SyncDepthRev,
			})
		}
	}
	result = append(result, corev1.EnvVar{
		Name:  gitSyncPeriod,
		Value: opts.period.String(),
	})
	// We can't use default values in git-sync because of the breaking change: https://github.com/kubernetes/git-sync/issues/841.
	// For backward compatibility, we set gitSyncRef to branch when ref is HEAD.
	// If ref is HEAD or empty,
	// - If branch is not set, set gitSyncRef to master (same behavior in git-sync v3).
	// - If branch is set, set gitSyncRef to user-specified branch.
	// If ref is neither HEAD nor empty, set gitSyncRef to ref regardless whether branch is set or not.
	adjustedRef := opts.ref
	if adjustedRef != "" && adjustedRef != "HEAD" {
		adjustedRef = opts.ref // sync from a commit or a tag if ref is not HEAD
	} else if opts.branch != "" {
		adjustedRef = opts.branch // sync from the HEAD of the user-provided branch
	} else {
		adjustedRef = "master" // sync from the HEAD of master for backward compatibility
	}
	result = append(result, corev1.EnvVar{
		Name:  gitSyncRef,
		Value: adjustedRef,
	})
	switch opts.secretType {
	case configsync.AuthGCENode, configsync.AuthGCPServiceAccount:
		result = append(result, corev1.EnvVar{
			Name:  gitSyncAskpassURL,
			Value: gceNodeAskpassURL,
		})
	case configsync.AuthSSH:
		result = append(result, corev1.EnvVar{
			Name:  gitSyncSSH,
			Value: "true",
		})
	case configsync.AuthCookieFile:
		result = append(result, corev1.EnvVar{
			Name:  gitSyncCookieFile,
			Value: "true",
		})

		fallthrough
	case GitSecretConfigKeyToken, "", configsync.AuthNone, configsync.AuthGithubApp:
		if opts.proxy != "" {
			result = append(result, corev1.EnvVar{
				Name:  gitSyncHTTPSProxy,
				Value: opts.proxy,
			})
		}
	default:
		return nil, fmt.Errorf("Unrecognized secret type %q", opts.secretType)
	}
	return result, nil
}
