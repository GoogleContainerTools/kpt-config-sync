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
	"os"
	"os/exec"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	csrRepo                    = "https://source.developers.google.com/p/stolos-dev/r/csr-auth-test"
	gsaCSRReaderEmail          = "e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com"
	syncBranch                 = "main"
	crossProjectFleetProjectID = "cs-dev-hub"
)

// TestGCENode tests the `gcenode` auth type.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
// 3. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` has `source.reader` access to Cloud Source Repository.
// Public documentation: https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync#git-creds-secret
func TestGCENode(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a CSR repo")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcenode", "secretRef": {"name": ""}}}}`,
		tenant, syncBranch, csrRepo))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`, origRepoURL))
	})

	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
	if err := validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationAbsent); err != nil {
		nt.T.Fatal(err)
	}
}

// TestGKEWorkloadIdentity tests the `gcpserviceaccount` auth type with GKE Workload Identity.
//  The test will run on a GKE cluster only with following pre-requisites
// 1. Workload Identity is enabled.
// 2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
// 3. The Google service account `e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com` is created with the `roles/source.reader` role to access to CSR.
// 4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//   gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//      --role roles/iam.workloadIdentityUser \
//      --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//      e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com
// 5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestGKEWorkloadIdentity(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  false,
		crossProject: false,
		sourceRepo:   csrRepo,
		sourceType:   v1beta1.GitSource,
		gsaEmail:     gsaCSRReaderEmail,
		rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
	})
}

// TestWorkloadIdentity tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//  The test will run on a GKE cluster only with following pre-requisites
// 1. Workload Identity is enabled.
// 2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
// 3. The Google service account `e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com` is created with the `roles/source.reader` role to access to CSR.
// 4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//   gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//      --role roles/iam.workloadIdentityUser \
//      --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//      e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com
// 5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t,
		workloadIdentityTestSpec{
			fleetWITest:  true,
			crossProject: false,
			sourceRepo:   csrRepo,
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gsaCSRReaderEmail,
			rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
		})
}

// TestFleetWIInDifferentProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (cross-project).
//  The test will run on a GKE cluster only with following pre-requisites
// 1. Workload Identity is enabled.
// 2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
// 3. The Google service account `e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com` is created with the `roles/source.reader` role to access to CSR.
// 4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//   gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//      --role roles/iam.workloadIdentityUser \
//      --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//      e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com
// 5. The cross-project fleet host project 'cs-dev-hub' is created.
// 6. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestFleetWIDifferentProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: true,
		sourceRepo:   csrRepo,
		sourceType:   v1beta1.GitSource,
		gsaEmail:     gsaCSRReaderEmail,
		rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
	})
}

// getMembershipIdentityProvider fetches the workload_identity_pool if the membership exists.
func getMembershipIdentityProvider(nt *nomostest.NT) (bool, string, error) {
	bytes, err := nt.Kubectl("get", "membership", "membership")
	out := string(bytes)
	if err != nil {
		if strings.Contains(out, `the server doesn't have a resource type "membership"`) || strings.Contains(out, "NotFound") {
			return false, "", nil
		}
		return false, "", fmt.Errorf("unable to get the membership %s: %w", out, err)
	}

	bytes, err = nt.Kubectl("get", "membership", "membership", "-o", "jsonpath={.spec.workload_identity_pool}")
	out = string(bytes)
	if err != nil {
		return true, "", fmt.Errorf("unable to get the membership workload identity %s: %w", out, err)
	}
	return true, out, nil
}

// cleanMembershipInfo deletes the membership by unregistering the cluster.
// It also deletes the reconciler-manager to ensure the membership watch is not set up.
func cleanMembershipInfo(nt *nomostest.NT, fleetMembership, gcpProject, gkeURI string) {
	membershipExists, wiPool, err := getMembershipIdentityProvider(nt)
	if err != nil {
		nt.T.Error(err)
		nt.T.Skip("Skip the test because unable to check if membership exists")
	} else if membershipExists {
		fleetProject := gcpProject
		if len(wiPool) > 0 {
			fleetProject = strings.TrimSuffix(wiPool, ".svc.id.goog")
		}
		nt.T.Logf("The membership exits, unregistering the cluster from project %q to clean up the membership", fleetProject)
		if err = unregisterCluster(fleetMembership, fleetProject, gkeURI); err != nil {
			nt.T.Errorf("failed to unregister the cluster: %v", err)
			nt.T.Skip("Skip the test because unable to unregister the cluster")
			membershipExists, _, err = getMembershipIdentityProvider(nt)
			if err != nil {
				nt.T.Error(err)
				nt.T.Skip("Skip the test because the unable to check if membership is deleted")
			} else if membershipExists {
				nt.T.Error("the membership should have been deleted")
				nt.T.Skip("Skip the test because the membership wasn't deleted")
			}
		}
		// b/226383057: DeletePodByLabel deletes the current reconciler-manager Pod so that new Pod
		// is guaranteed to have no membership watch setup.
		// This is to ensure consistent behavior when the membership is not cleaned up from previous runs.
		// The underlying reconciler may or may not be restarted depending on the membership existence.
		// If membership exists before the reconciler-manager is deployed (test leftovers), the reconciler will be updated.
		// If membership doesn't exist (normal scenario), the reconciler should remain the same after the reconciler-manager restarts.
		nt.T.Logf("Restart the reconciler-manager to ensure the membership watch is not set up")
		nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, false)
		nomostest.Wait(nt.T, "wait for FWI credentials to be absent", nt.DefaultWaitTimeout, func() error {
			return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationAbsent)
		})
	}
}

type workloadIdentityTestSpec struct {
	fleetWITest  bool
	crossProject bool
	sourceRepo   string
	sourceType   v1beta1.SourceType
	gsaEmail     string
	rootCommitFn nomostest.Sha1Func
}

func testWorkloadIdentity(t *testing.T, testSpec workloadIdentityTestSpec) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured, ntopts.RequireGKE(t))

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	gcpProject := os.Getenv("GCP_PROJECT")
	if gcpProject == "" {
		t.Fatal("Environment variable 'GCP_PROJECT' is required for this test case")
	}
	gcpCluster := os.Getenv("GCP_CLUSTER")
	if gcpCluster == "" {
		t.Fatal("Environment variable 'GCP_CLUSTER' is required for this test case")
	}
	gkeRegion := os.Getenv("GCP_REGION")
	gkeZone := os.Getenv("GCP_ZONE")
	if gkeRegion == "" && gkeZone == "" {
		t.Fatal("Environment variable 'GCP_REGION' or 'GCP_ZONE' is required for this test case")
	}
	fleetMembership := fmt.Sprintf("%s-%s", gcpProject, gcpCluster)
	gkeURI := "https://container.googleapis.com/v1/projects/" + gcpProject
	if gkeRegion != "" {
		gkeURI += fmt.Sprintf("/locations/%s/clusters/%s", gkeRegion, gcpCluster)
	} else {
		gkeURI += fmt.Sprintf("/zones/%s/clusters/%s", gkeZone, gcpCluster)
	}

	cleanMembershipInfo(nt, fleetMembership, gcpProject, gkeURI)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Cleanup(func() {
		// Change the rs back so that the remaining tests can run in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
		cleanMembershipInfo(nt, fleetMembership, gcpProject, gkeURI)
	})

	tenant := "tenant-a"

	// Register the cluster for fleet workload identity test
	if testSpec.fleetWITest {
		fleetProject := gcpProject
		if testSpec.crossProject {
			fleetProject = crossProjectFleetProjectID
		}
		nt.T.Logf("Register the cluster to a fleet in project %q", fleetProject)
		if err := registerCluster(fleetMembership, fleetProject, gkeURI); err != nil {
			nt.T.Errorf("failed to register the cluster: %v", err)
			nt.T.Skipf("Skip the test because unable to register the cluster to project %q", fleetProject)
			membershipExists, _, err := getMembershipIdentityProvider(nt)
			if err != nil {
				nt.T.Error(err)
				nt.T.Skip("Skip the test because unable to check if membership exists")
			} else if !membershipExists {
				nt.T.Error("the membership should be created, but not")
				nt.T.Skip("Skip the test because the membership has not been created")
			}
		}
		nt.T.Logf("Restart the reconciler-manager to pick up the Membership")
		// The reconciler manager checks if the Membership CRD exists before setting
		// up the RootSync and RepoSync controllers: cmd/reconciler-manager/main.go:90.
		// If the CRD exists, it configures the Membership watch.
		// Otherwise, the watch is not configured to prevent the controller from crashing caused by an unknown CRD.
		// DeletePodByLabel deletes the current reconciler-manager Pod so that new Pod
		// can set up the watch. Once the watch is configured, it can detect the
		// deletion and creation of the Membership, which implies cluster unregistration and registration.
		// The underlying reconciler should be updated with FWI creds after the reconciler-manager restarts.
		nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, false)
	}

	// Reuse the RootSync instead of creating a new one so that testing resources can be cleaned up after the test.
	nt.T.Logf("Update RootSync to sync %s from repo %s", tenant, testSpec.sourceRepo)
	if testSpec.sourceType == v1beta1.GitSource {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "secretRef": {"name": ""}}}}`,
			tenant, syncBranch, testSpec.sourceRepo, testSpec.gsaEmail))
	} else {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s"}, "git": null}}`,
			v1beta1.OciSource, tenant, testSpec.sourceRepo, testSpec.gsaEmail))
	}

	if testSpec.fleetWITest {
		nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
			return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationExists)
		})
	}

	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(testSpec.rootCommitFn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
}

// validateFWICredentials validates whether the reconciler Pod manifests includes
// the FWI credentials annotation or not.
func validateFWICredentials(nt *nomostest.NT, reconcilerName string, validationFn func(pod corev1.Pod) error) error {
	var podList = &corev1.PodList{}
	if err := nt.List(podList, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{metadata.ReconcilerLabel: reconcilerName}); err != nil {
		return err
	}
	if len(podList.Items) != 1 {
		return fmt.Errorf("expected only 1 Pod for the reconciler %s, but got %d", reconcilerName, len(podList.Items))
	}
	return validationFn(podList.Items[0])
}

// fwiAnnotationAbsent validates if the Pod has the FWI credentials annotation.
func fwiAnnotationExists(pod corev1.Pod) error {
	if _, found := pod.GetAnnotations()[metadata.FleetWorkloadIdentityCredentials]; !found {
		return fmt.Errorf("object %s/%s does not have annotation %q", pod.GetNamespace(), pod.GetName(), metadata.FleetWorkloadIdentityCredentials)
	}
	return nil
}

// fwiAnnotationAbsent validates if the Pod doesn't have the FWI credentials annotation.
func fwiAnnotationAbsent(pod corev1.Pod) error {
	if _, found := pod.GetAnnotations()[metadata.FleetWorkloadIdentityCredentials]; found {
		return fmt.Errorf("object %s/%s has annotation %q", pod.GetNamespace(), pod.GetName(), metadata.FleetWorkloadIdentityCredentials)
	}
	return nil
}

// unregisterCluster unregisters a cluster from a fleet.
func unregisterCluster(fleetMembership, gcpProject, gkeURI string) error {
	out, err := exec.Command("gcloud", "container", "hub", "memberships", "unregister", fleetMembership, "--quiet", "--project", gcpProject, "--gke-uri", gkeURI).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v", string(out), err)
	}
	return nil
}

// registerCluster registers a cluster in a fleet.
func registerCluster(fleetMembership, gcpProject, gkeURI string) error {
	out, err := exec.Command("gcloud", "container", "hub", "memberships", "register", fleetMembership, "--project", gcpProject, "--gke-uri", gkeURI, "--enable-workload-identity").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v", string(out), err)
	}
	return nil
}
