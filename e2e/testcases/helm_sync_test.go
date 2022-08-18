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

package e2e

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	privateARHelmRegistry   = "oci://us-docker.pkg.dev/stolos-dev/configsync-ci-ar-helm"
	privateHelmChartVersion = "1.13.3"
	privateHelmChart        = "coredns"
)

// TestPublicHelm can run on both Kind and GKE clusters.
// It tests Config Sync can pull from public Helm repo without any authentication.
func TestPublicHelm(t *testing.T) {
	publicHelmRepo := "https://kubernetes.github.io/ingress-nginx"
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured)
	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a public Helm Chart")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "ingress-nginx", "auth": "none", "version": "4.0.5", "releaseName": "my-ingress-nginx", "namespace": "ingress-nginx"}, "git": null}}`,
		v1beta1.HelmSource, publicHelmRepo))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(helmChartVersion("ingress-nginx:4.0.5")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "ingress-nginx"}))
	if err := nt.Validate("my-ingress-nginx-controller", "ingress-nginx", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmARFleetWISameProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestHelmARFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:   true,
		crossProject:  false,
		sourceRepo:    privateARHelmRegistry,
		sourceVersion: privateHelmChartVersion,
		sourceChart:   privateHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail,
		rootCommitFn:  helmChartVersion(privateHelmChart + ":" + privateHelmChartVersion),
	})
}

// TestHelmARTokenAuth verifies Config Sync can pull Helm chart from private Artifact Registry with Token auth type.
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
// A JSON key file is generated for this service account and stored in Secret Manager
func TestHelmARTokenAuth(t *testing.T) {
	nt := nomostest.New(t,
		ntopts.SkipMonoRepo,
		ntopts.Unstructured,
		ntopts.RequireGKE(t),
	)
	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Fetch password from Secret Manager")
	key, err := gitproviders.FetchCloudSecret("config-sync-ci-ar-key")
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Create secret for authentication")
	_, err = nt.Kubectl("create", "secret", "generic", "foo", "--namespace=config-management-system", "--from-literal=username=_json_key", fmt.Sprintf("--from-literal=password=%s", key))
	if err != nil {
		nt.T.Fatalf("failed to create secret, err: %v", err)
	}
	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "%s", "auth": "token", "version": "%s", "releaseName": "my-coredns", "namespace": "coredns", "secretRef": {"name" : "foo"}}, "git": null}}`,
		v1beta1.HelmSource, privateARHelmRegistry, privateHelmChart, privateHelmChartVersion))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(helmChartVersion(privateHelmChart+":"+privateHelmChartVersion)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: privateHelmChart}))
	if err := nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		containerImagePullPolicy("IfNotPresent")); err != nil {
		nt.T.Error(err)
	}
}

func helmChartVersion(chartVersion string) nomostest.Sha1Func {
	return func(*nomostest.NT, types.NamespacedName) (string, error) {
		return chartVersion, nil
	}
}
