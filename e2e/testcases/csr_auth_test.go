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
	csrRepo    = "https://source.developers.google.com/p/stolos-dev/r/csr-auth-test"
	syncBranch = "main"
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

// TestWorkloadIdentity tests the `gcpserviceaccount` auth type with both GKE
// Workload Identity and Fleet Workload Identity (in-project and cross-project).
//  The test will run on a GKE cluster only with following pre-requisites
// 1. Workload Identity is enabled.
// 2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
// 3. The Google service account `e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com` is created with the `roles/source.reader` role to access to CSR.
// 4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//   gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//      --role roles/iam.workloadIdentityUser \
//      --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//      --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//      e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com
// 5. The cross-project fleet host project 'cs-dev-hub' is created.
// 6. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestWorkloadIdentity(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured, ntopts.RequireGKE(t))

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	crossProjectFleetProjectID := "cs-dev-hub"
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

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Cleanup(func() {
		// Change the rs back so that the remaining tests can run in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`, origRepoURL))
		// Unregister the cluster in the same project.
		if err := unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
			nt.T.Log(err)
		}
		// Unregister the cluster in a different fleet host project.
		if err := unregisterCluster(fleetMembership, crossProjectFleetProjectID, gkeURI); err != nil {
			nt.T.Log(err)
		}
	})

	gsaEmail := "e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com"
	testWorkloadIdentity(nt, fleetMembership, gcpProject, crossProjectFleetProjectID, gkeURI, csrRepo, gsaEmail, v1beta1.GitSource, nomostest.RemoteRootRepoSha1Fn)
}

func testWorkloadIdentity(nt *nomostest.NT, fleetMembership, gcpProject, crossProjectFleetProjectID, gkeURI, repo, gsaEmail string,
	sourceType v1beta1.SourceType, rootCommitFn nomostest.Sha1Func) {
	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Logf("Update RootSync to sync %s from repo %s", tenant, repo)
	if sourceType == v1beta1.GitSource {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "secretRef": {"name": ""}}}}`,
			tenant, syncBranch, repo, gsaEmail))
	} else {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s"}, "git": null}}`,
			v1beta1.OciSource, tenant, repo, gsaEmail))
	}

	nt.T.Log("Verify Fleet workload identity within the same project")
	nt.T.Log("Unregister the cluster if it is registered in the same project or a different project")
	if err := unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
		nt.T.Fatal(err)
	}
	if err := unregisterCluster(fleetMembership, crossProjectFleetProjectID, gkeURI); err != nil {
		nt.T.Fatal(err)
	}
	// DeletePodByLabel deletes the current reconciler-manager Pod so that new Pod
	// is guaranteed to have no membership watch setup.
	// This is to ensure consistent behavior when the membership is not cleaned up from previous run.
	// The underlying reconciler may or may not be restarted depending on the membership existence.
	// If membership exists before the reconciler-manager is deployed (test leftovers), the reconciler will be updated.
	// If membership doesn't exist (normal scenario), the reconciler should remain the same after the reconciler-manager restarts.
	nt.T.Logf("Restart the reconciler-manager to ensure the membership watch is not set up")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, false)
	nt.T.Log("Register the cluster to a fleet in the same project")
	if err := registerCluster(fleetMembership, gcpProject, gkeURI); err != nil {
		nt.T.Fatal(err)
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
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)
	nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
		return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationExists)
	})

	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(rootCommitFn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)

	nt.T.Log("Verify GKE workload identity")
	nt.T.Log("Unregister the cluster from the fleet in the same project")
	if err := unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
		nt.T.Fatal(err)
	}
	tenant = "tenant-b"
	nt.T.Logf("Update RootSync to sync %s", tenant)
	if sourceType == v1beta1.GitSource {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s"}}}`, tenant))
	} else {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"dir": "%s"}}}`, tenant))
	}
	nomostest.Wait(nt.T, "wait for FWI credentials to be absent", nt.DefaultWaitTimeout, func() error {
		return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationAbsent)
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(rootCommitFn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)

	nt.T.Log("Verify Fleet workload identity across project boundary")
	nt.T.Log("Register the cluster to a fleet in a different project")
	if err := registerCluster(fleetMembership, crossProjectFleetProjectID, gkeURI); err != nil {
		nt.T.Fatal(err)
	}
	tenant = "tenant-c"
	nt.T.Logf("Update RootSync to sync %s", tenant)
	if sourceType == v1beta1.GitSource {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s"}}}`, tenant))
	} else {
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"dir": "%s"}}}`, tenant))
	}
	nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
		return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationExists)
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(rootCommitFn),
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
