package e2e

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	privateARHelmRegistry   = "us-docker.pkg.dev/stolos-dev/configsync-ci-ar-helm/"
	privateHelmChartVersion = "1.16.4"
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

func helmChartVersion(chartVersion string) nomostest.Sha1Func {
	return func(*nomostest.NT, types.NamespacedName) (string, error) {
		return chartVersion, nil
	}
}
