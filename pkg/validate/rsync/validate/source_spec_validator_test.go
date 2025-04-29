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
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/testerrors"
)

func auth(authType configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Auth = authType
	}
}

func ociAuth(authType configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Oci.Auth = authType
	}
}

func helmAuth(authType configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Helm.Auth = authType
	}
}

func named(name string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Name = name
	}
}

func proxy(proxy string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Proxy = proxy
	}
}

func secret(secretName string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.SecretRef = &v1beta1.SecretReference{
			Name: secretName,
		}
	}
}

func gcpSAEmail(email string) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.GCPServiceAccountEmail = email
	}
}

func missingRepo(rs *v1beta1.RepoSync) {
	rs.Spec.Repo = ""
}

func missingImage(rs *v1beta1.RepoSync) {
	rs.Spec.Oci.Image = ""
}

func missingHelmRepo(rs *v1beta1.RepoSync) {
	rs.Spec.Helm.Repo = ""
}

func missingHelmChart(rs *v1beta1.RepoSync) {
	rs.Spec.Helm.Chart = ""
}

func repoSyncWithGit(opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName)
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo: "fake repo",
		Auth: configsync.AuthNone,
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func repoSyncWithOci(opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName)
	rs.Spec.SourceType = configsync.OciSource
	rs.Spec.Oci = &v1beta1.Oci{
		Image: "fake image",
		Auth:  configsync.AuthNone,
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func repoSyncWithHelm(opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName)
	rs.Spec.SourceType = configsync.HelmSource
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
		Repo:  "fake repo",
		Chart: "fake chart",
		Auth:  configsync.AuthNone,
	}}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func withGit() func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Git = &v1beta1.Git{
			Auth: configsync.AuthNone,
		}
	}
}

func withOci() func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Oci = &v1beta1.Oci{
			Auth: configsync.AuthNone,
		}
	}
}

func withHelm() func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Helm = &v1beta1.HelmRepoSync{
			HelmBase: v1beta1.HelmBase{
				Auth: configsync.AuthNone,
			},
		}
	}
}

func rootSyncWithGit(opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo: "fake repo",
		Auth: configsync.AuthNone,
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func rootSyncWithHelm(opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	rs.Spec.SourceType = configsync.HelmSource
	rs.Spec.Helm = &v1beta1.HelmRootSync{HelmBase: v1beta1.HelmBase{
		Repo:  "fake repo",
		Chart: "fake chart",
		Auth:  configsync.AuthNone,
	}}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func TestValidateRepoSyncSpec(t *testing.T) {
	testCases := []struct {
		name    string
		obj     *v1beta1.RepoSync
		wantErr status.Error
	}{
		// Validate Git Spec
		{
			name: "valid git",
			obj:  repoSyncWithGit(),
		},
		{
			name: "a user-defined name",
			obj:  repoSyncWithGit(named("user-defined-repo-sync-name")),
		},
		{
			name:    "missing git repo",
			obj:     repoSyncWithGit(missingRepo),
			wantErr: MissingGitRepo(configsync.RepoSyncKind),
		},
		{
			name:    "invalid git auth type",
			obj:     repoSyncWithGit(auth("invalid auth")),
			wantErr: InvalidGitAuthType(configsync.RepoSyncKind),
		},
		{
			name:    "no op proxy",
			obj:     repoSyncWithGit(auth(configsync.AuthGCENode), proxy("no-op proxy")),
			wantErr: NoOpProxy(configsync.RepoSyncKind),
		},
		{
			name: "valid proxy with none auth type",
			obj:  repoSyncWithGit(proxy("ok proxy")),
		},
		{
			name: "valid proxy with cookiefile",
			obj:  repoSyncWithGit(auth(configsync.AuthCookieFile), secret("cookiefile"), proxy("ok proxy")),
		},
		{
			name: "valid proxy with token",
			obj:  repoSyncWithGit(auth(configsync.AuthToken), secret("token"), proxy("ok proxy")),
		},
		{
			name:    "illegal secret",
			obj:     repoSyncWithGit(secret("illegal secret")),
			wantErr: IllegalSecretRef(configsync.GitSource, configsync.RepoSyncKind),
		},
		{
			name:    "missing secret",
			obj:     repoSyncWithGit(auth(configsync.AuthSSH)),
			wantErr: MissingSecretRef(configsync.GitSource, configsync.RepoSyncKind),
		},
		{
			name:    "invalid GCP serviceaccount email",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("invalid_gcp_sa@gserviceaccount.com")),
			wantErr: InvalidGCPSAEmail(configsync.GitSource, configsync.RepoSyncKind),
		},
		{
			name:    "invalid GCP serviceaccount email with correct suffix",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("foo@my-project.iam.gserviceaccount.com")),
			wantErr: InvalidGCPSAEmail(configsync.GitSource, configsync.RepoSyncKind),
		},
		{
			name:    "invalid GCP serviceaccount email without domain",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("my-project")),
			wantErr: InvalidGCPSAEmail(configsync.GitSource, configsync.RepoSyncKind),
		},
		{
			name:    "missing GCP serviceaccount email for git",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount)),
			wantErr: MissingGCPSAEmail(configsync.GitSource, configsync.RepoSyncKind),
		},
		// Validate OCI spec
		{
			name: "valid oci",
			obj:  repoSyncWithOci(),
		},
		{
			name:    "missing oci image",
			obj:     repoSyncWithOci(missingImage),
			wantErr: MissingOciImage(configsync.RepoSyncKind),
		},
		{
			name:    "invalid auth type",
			obj:     repoSyncWithOci(ociAuth("invalid auth")),
			wantErr: InvalidOciAuthType(configsync.RepoSyncKind),
		},
		{
			name:    "missing GCP serviceaccount email for Oci",
			obj:     repoSyncWithOci(ociAuth(configsync.AuthGCPServiceAccount)),
			wantErr: MissingGCPSAEmail(configsync.OciSource, configsync.RepoSyncKind),
		},
		{
			name:    "invalid source type",
			obj:     k8sobjects.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName, k8sobjects.WithRepoSyncSourceType("invalid")),
			wantErr: InvalidSourceType(configsync.RepoSyncKind),
		},
		{
			name:    "redundant OCI spec",
			obj:     repoSyncWithGit(withOci()),
			wantErr: nil,
		},
		{
			name:    "redundant Git spec",
			obj:     repoSyncWithOci(withGit()),
			wantErr: nil,
		},
		// Validate Helm spec
		{
			name: "valid helm",
			obj:  repoSyncWithHelm(),
		},
		{
			name:    "missing helm repo",
			obj:     repoSyncWithHelm(missingHelmRepo),
			wantErr: MissingHelmRepo(configsync.RepoSyncKind),
		},
		{
			name:    "missing helm chart",
			obj:     repoSyncWithHelm(missingHelmChart),
			wantErr: MissingHelmChart(configsync.RepoSyncKind),
		},
		{
			name:    "invalid auth type",
			obj:     repoSyncWithHelm(helmAuth("invalid auth")),
			wantErr: InvalidHelmAuthType(configsync.RepoSyncKind),
		},
		{
			name:    "missing GCP serviceaccount email for Helm",
			obj:     repoSyncWithHelm(helmAuth(configsync.AuthGCPServiceAccount)),
			wantErr: MissingGCPSAEmail(configsync.HelmSource, configsync.RepoSyncKind),
		},
		{
			name:    "redundant Helm spec",
			obj:     repoSyncWithGit(withHelm()),
			wantErr: nil,
		},
		{
			name: "valid spec.override.resources",
			obj: repoSyncWithGit(func(rs *v1beta1.RepoSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						ContainerName: "test-name",
						CPURequest:    resource.MustParse("100m"),
						CPULimit:      resource.MustParse("1000m"),
						MemoryRequest: resource.MustParse("100Mi"),
						MemoryLimit:   resource.MustParse("1Gi"),
					},
				}
			}),
			wantErr: nil,
		},
		{
			name: "invalid spec.override.resources.cpuRequest",
			obj: repoSyncWithGit(func(rs *v1beta1.RepoSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						CPURequest: resource.MustParse("-100m"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("cpuRequest", configsync.RepoSyncKind),
		},
		{
			name: "invalid spec.override.resources.cpuLimit",
			obj: repoSyncWithGit(func(rs *v1beta1.RepoSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						CPULimit: resource.MustParse("-1000m"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("cpuLimit", configsync.RepoSyncKind),
		},
		{
			name: "invalid spec.override.resources.memoryRequest",
			obj: repoSyncWithGit(func(rs *v1beta1.RepoSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						MemoryRequest: resource.MustParse("-100Mi"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("memoryRequest", configsync.RepoSyncKind),
		},
		{
			name: "invalid spec.override.resources.memoryLimit",
			obj: repoSyncWithGit(func(rs *v1beta1.RepoSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						MemoryLimit: resource.MustParse("-1Gi"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("memoryLimit", configsync.RepoSyncKind),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := RepoSyncSpec(tc.obj.Spec)
			testerrors.AssertEqual(t, tc.wantErr, err)
		})
	}
}

func TestValidateRootSyncSpec(t *testing.T) {
	testCases := []struct {
		name    string
		obj     *v1beta1.RootSync
		wantErr status.Error
	}{
		{
			name: "valid git",
			obj:  rootSyncWithGit(),
		},
		{
			name: "valid spec.helm.namespace",
			obj: rootSyncWithHelm(func(sync *v1beta1.RootSync) {
				sync.Spec.Helm.Namespace = "test-ns"
				sync.Spec.Helm.DeployNamespace = ""
			}),
			wantErr: nil,
		},
		{
			name: "valid spec.helm.deployNamespace",
			obj: rootSyncWithHelm(func(sync *v1beta1.RootSync) {
				sync.Spec.Helm.Namespace = ""
				sync.Spec.Helm.DeployNamespace = "test-ns"
			}),
			wantErr: nil,
		},
		{
			name: "invalid spec.helm.namespace and spec.helm.deployNamespace",
			obj: rootSyncWithHelm(func(sync *v1beta1.RootSync) {
				sync.Spec.Helm.Namespace = "test-ns"
				sync.Spec.Helm.DeployNamespace = "test-ns"
			}),
			wantErr: HelmNSAndDeployNS(configsync.RootSyncKind),
		},
		{
			name: "valid spec.override.roleRefs Role",
			obj: rootSyncWithGit(func(sync *v1beta1.RootSync) {
				sync.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
					{
						Kind:      "Role",
						Name:      "test-role",
						Namespace: "test-ns",
					},
				}
			}),
			wantErr: nil,
		},
		{
			name: "valid spec.override.roleRefs ClusterRole",
			obj: rootSyncWithGit(func(sync *v1beta1.RootSync) {
				sync.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
					{
						Kind: "ClusterRole",
						Name: "test-role",
					},
				}
			}),
			wantErr: nil,
		},
		{
			name: "valid spec.override.roleRefs Role and ClusterRole",
			obj: rootSyncWithGit(func(sync *v1beta1.RootSync) {
				sync.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
					{
						Kind:      "Role",
						Name:      "test-role",
						Namespace: "test-ns",
					},
					{
						Kind: "ClusterRole",
						Name: "test-role",
					},
				}
			}),
			wantErr: nil,
		},
		{
			name: "missing spec.override.roleRefs Role namespace",
			obj: rootSyncWithGit(func(sync *v1beta1.RootSync) {
				sync.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
					{
						Kind: "Role",
						Name: "test-role",
					},
				}
			}),
			wantErr: OverrideRoleRefNamespace(configsync.RootSyncKind),
		},
		{
			name: "valid spec.override.resources",
			obj: rootSyncWithGit(func(rs *v1beta1.RootSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						ContainerName: "test-name",
						CPURequest:    resource.MustParse("100m"),
						CPULimit:      resource.MustParse("1000m"),
						MemoryRequest: resource.MustParse("100Mi"),
						MemoryLimit:   resource.MustParse("1Gi"),
					},
				}
			}),
			wantErr: nil,
		},
		{
			name: "invalid spec.override.resources.cpuRequest",
			obj: rootSyncWithGit(func(rs *v1beta1.RootSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						CPURequest: resource.MustParse("-100m"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("cpuRequest", configsync.RootSyncKind),
		},
		{
			name: "invalid spec.override.resources.cpuLimit",
			obj: rootSyncWithGit(func(rs *v1beta1.RootSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						CPULimit: resource.MustParse("-1000m"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("cpuLimit", configsync.RootSyncKind),
		},
		{
			name: "invalid spec.override.resources.memoryRequest",
			obj: rootSyncWithGit(func(rs *v1beta1.RootSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						MemoryRequest: resource.MustParse("-100Mi"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("memoryRequest", configsync.RootSyncKind),
		},
		{
			name: "invalid spec.override.resources.memoryLimit",
			obj: rootSyncWithGit(func(rs *v1beta1.RootSync) {
				rs.Spec.SafeOverride().Resources = []v1beta1.ContainerResourcesSpec{
					{
						MemoryLimit: resource.MustParse("-1Gi"),
					},
				}
			}),
			wantErr: OverrideResourceQuantityNegative("memoryLimit", configsync.RootSyncKind),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := RootSyncSpec(tc.obj.Spec)
			testerrors.AssertEqual(t, tc.wantErr, err)
		})
	}
}
