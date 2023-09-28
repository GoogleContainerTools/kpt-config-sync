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
	"k8s.io/klog/v2"
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

	// gitSyncKnownHosts represents the environment variable key for GIT_KNOWN_HOSTS.
	gitSyncKnownHosts = "GITSYNC_SSH_KNOWN_HOSTS"
	// GitSSLNoVerify represents the environment variable key for GIT_SSL_NO_VERIFY.
	GitSSLNoVerify = "GIT_SSL_NO_VERIFY"

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
)

var gceNodeAskpassURL = fmt.Sprintf("http://localhost:%v/git_askpass", gceNodeAskpassPort)

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

func gitSyncEnvs(_ context.Context, opts options) []corev1.EnvVar {
	var result []corev1.EnvVar
	result = append(result, corev1.EnvVar{
		Name:  GitSyncRepo,
		Value: opts.repo,
	})
	// disable known_hosts checking because it provides no benefit for our use case.
	result = append(result, corev1.EnvVar{
		Name:  gitSyncKnownHosts,
		Value: "false",
	})
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
	// When branch and ref not set in RootSync/RepoSync then dont set GITSYNC_REF,
	// git-sync will use the default values for them.
	// Both opts.ref and opts.branch map to GITSYNC_REF, but opts.ref takes precedence over opts.branch.
	if opts.ref != "" {
		result = append(result, corev1.EnvVar{
			Name:  gitSyncRef,
			Value: opts.ref,
		})
	} else if opts.branch != "" {
		result = append(result, corev1.EnvVar{
			Name:  gitSyncRef,
			Value: opts.branch,
		})
	}
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
	case GitSecretConfigKeyToken, "", configsync.AuthNone:
		if opts.proxy != "" {
			result = append(result, corev1.EnvVar{
				Name:  gitSyncHTTPSProxy,
				Value: opts.proxy,
			})
		}
	default:
		// TODO b/168553377 Return error while setting up gitSyncData.
		klog.Errorf("Unrecognized secret type %s", opts.secretType)
	}
	return result
}
