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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/helm"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
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
	syncBranch                 = "main"
	crossProjectFleetProjectID = "cs-dev-hub"
)

// The CSR repo was built from the directory e2e/testdata/hydration/kustomize-components,
// which includes a kustomization.yaml file in the root directory that
// references resources for tenant-a, tenant-b, and tenant-c.
// Each tenant includes a NetworkPolicy, a Role and a RoleBinding.
func csrRepo() string {
	return fmt.Sprintf("https://source.developers.google.com/p/%s/r/kustomize-components", *e2e.GCPProject)
}

func gsaCSRReaderEmail() string {
	return fmt.Sprintf("e2e-test-csr-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// TestGCENode tests the `gcenode` auth type.
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is NOT enabled
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` has `source.reader`
//     access to Cloud Source Repository.
//
// Public documentation:
// https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync#git-creds-secret
func TestGCENode(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	if workloadPool, err := getWorkloadPool(nt); err != nil {
		nt.T.Fatal(err)
	} else if workloadPool != "" {
		nt.T.Fatal("expected workload identity to be disabled")
	}

	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a CSR repo")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcenode", "secretRef": {"name": ""}}}}`,
		tenant, syncBranch, csrRepo()))

	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	if err != nil {
		nt.T.Fatal(err)
	}
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
	if err := validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationAbsent); err != nil {
		nt.T.Fatal(err)
	}
}

// TestGKEWorkloadIdentity tests the `gcpserviceaccount` auth type with GKE Workload Identity.
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is enabled.
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Google service account `e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with the
//     `roles/source.reader` role to access to CSR.
//  4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the
//     `roles/iam.workloadIdentityUser` role.
//     gcloud iam service-accounts add-iam-policy-binding --project=${GCP_PROJECT} \
//     --role roles/iam.workloadIdentityUser \
//     --member "serviceAccount:${GCP_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
//     e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com
//  5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestGKEWorkloadIdentity(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  false,
		crossProject: false,
		sourceRepo:   csrRepo(),
		sourceType:   v1beta1.GitSource,
		gsaEmail:     gsaCSRReaderEmail(),
		rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
	})
}

// TestWorkloadIdentity tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is enabled.
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Google service account `e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with the
//     `roles/source.reader` role to access to CSR.
//  4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the
//     `roles/iam.workloadIdentityUser` role.
//     gcloud iam service-accounts add-iam-policy-binding --project=${GCP_PROJECT} \
//     --role roles/iam.workloadIdentityUser \
//     --member "serviceAccount:${GCP_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
//     e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com
//  5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t,
		workloadIdentityTestSpec{
			fleetWITest:  true,
			crossProject: false,
			sourceRepo:   csrRepo(),
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gsaCSRReaderEmail(),
			rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
		})
}

// TestFleetWIInDifferentProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (cross-project).
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is enabled.
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Google service account `e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with the
//     `roles/source.reader` role to access to CSR.
//  4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the
//     `roles/iam.workloadIdentityUser` role.
//     gcloud iam service-accounts add-iam-policy-binding --project=${GCP_PROJECT} \
//     --role roles/iam.workloadIdentityUser \
//     --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//     e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com
//  5. The cross-project fleet host project 'cs-dev-hub' is created.
//  6. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestFleetWIDifferentProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: true,
		sourceRepo:   csrRepo(),
		sourceType:   v1beta1.GitSource,
		gsaEmail:     gsaCSRReaderEmail(),
		rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
	})
}

// getMembership gets the membership resource in the GCP project.
// It returns a boolean that indicates whether the membership exists, and an error
func getMembership(fleetMembership, gcpProject string) (bool, error) {
	bytes, err := exec.Command("gcloud", "container", "fleet", "memberships", "describe", fleetMembership, "--project", gcpProject).CombinedOutput()
	out := string(bytes)
	if err != nil {
		// There are different error messages returned from the fleet:
		// - ERROR: (gcloud.container.fleet.memberships.describe) Membership xxx not found in the fleet.
		// - ERROR: (gcloud.container.fleet.memberships.describe) No memberships available in the fleet.
		// - ERROR: (gcloud.container.fleet.memberships.describe) NOT_FOUND: Resource 'projects/gcpProject/locations/global/memberships/fleetMembership' was not found
		if strings.Contains(out, fmt.Sprintf("Membership %s not found in the fleet", fleetMembership)) ||
			strings.Contains(out, "No memberships available in the fleet") ||
			strings.Contains(out, "NOT_FOUND") {
			return false, nil
		}
		return false, fmt.Errorf("failed to describe the membership %s in project %s (out: %s\nerror: %w)", fleetMembership, gcpProject, out, err)
	}

	return true, nil
}

// cleanMembershipInfo deletes the membership by unregistering the cluster.
// It also deletes the reconciler-manager to ensure the membership watch is not set up.
func cleanMembershipInfo(nt *nomostest.NT, fleetMembership, gcpProject, gkeURI string) {
	exists, err := getMembership(fleetMembership, gcpProject)
	if err != nil {
		nt.T.Fatalf("Unable to check the membership: %v", err)
	}

	if !exists {
		return
	}

	nt.T.Logf("The membership exits, unregistering the cluster from project %q to clean up the membership", gcpProject)
	if err = unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
		nt.T.Logf("Failed to unregister the cluster: %v", err)
		if err = deleteMembership(fleetMembership, gcpProject); err != nil {
			nt.T.Logf("Failed to delete the membership %q: %v", fleetMembership, err)
		}
		exists, err = getMembership(fleetMembership, gcpProject)
		if err != nil {
			nt.T.Fatalf("Unable to check if membership is deleted: %v", err)
		}
		if exists {
			nt.T.Fatalf("The membership wasn't deleted")
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

type workloadIdentityTestSpec struct {
	fleetWITest   bool
	crossProject  bool
	sourceRepo    string
	sourceChart   string
	sourceVersion string
	sourceType    v1beta1.SourceType
	gsaEmail      string
	rootCommitFn  nomostest.Sha1Func
}

func truncateStringByLength(s string, l int) string {
	if len(s) > l {
		return s[:l]
	}
	return s
}

func testWorkloadIdentity(t *testing.T, testSpec workloadIdentityTestSpec) {
	nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.Unstructured, ntopts.RequireGKE(t))

	// Verify workload identity is enabled on the cluster
	expectedPool := fmt.Sprintf("%s.svc.id.goog", *e2e.GCPProject)
	if workloadPool, err := getWorkloadPool(nt); err != nil {
		nt.T.Fatal(err)
	} else if workloadPool != expectedPool {
		nt.T.Fatalf("expected workloadPool %s but got %s", expectedPool, workloadPool)
	}

	// Truncate the fleetMembership length to be at most 63 characters.
	fleetMembership := truncateStringByLength(fmt.Sprintf("%s-%s", truncateStringByLength(*e2e.GCPProject, 20), nt.ClusterName), 63)
	gkeURI := "https://container.googleapis.com/v1/projects/" + *e2e.GCPProject
	if *e2e.GCPRegion != "" {
		gkeURI += fmt.Sprintf("/locations/%s/clusters/%s", *e2e.GCPRegion, nt.ClusterName)
	} else {
		gkeURI += fmt.Sprintf("/zones/%s/clusters/%s", *e2e.GCPZone, nt.ClusterName)
	}

	cleanMembershipInfo(nt, fleetMembership, *e2e.GCPProject, gkeURI)
	cleanMembershipInfo(nt, fleetMembership, crossProjectFleetProjectID, gkeURI)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Cleanup(func() {
		cleanMembershipInfo(nt, fleetMembership, *e2e.GCPProject, gkeURI)
		cleanMembershipInfo(nt, fleetMembership, crossProjectFleetProjectID, gkeURI)
	})

	tenant := "tenant-a"

	// Register the cluster for fleet workload identity test
	if testSpec.fleetWITest {
		fleetProject := *e2e.GCPProject
		if testSpec.crossProject {
			fleetProject = crossProjectFleetProjectID
		}
		nt.T.Logf("Register the cluster to a fleet in project %q", fleetProject)
		if err := registerCluster(fleetMembership, fleetProject, gkeURI); err != nil {
			nt.T.Fatalf("Failed to register the cluster to project %q: %v", fleetProject, err)
			exists, err := getMembership(fleetMembership, fleetProject)
			if err != nil {
				nt.T.Fatalf("Unable to check if membership exists: %v", err)
			}
			if !exists {
				nt.T.Fatalf("The membership wasn't created")
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

	// For helm charts, we need to push the chart to the AR before configuring the RootSync
	if testSpec.sourceType == v1beta1.HelmSource {
		remoteHelmChart, err := helm.PushHelmChart(nt, privateCoreDNSHelmChart, privateCoreDNSHelmChartVersion)
		if err != nil {
			nt.T.Fatalf("failed to push helm chart: %v", err)
		}

		testSpec.sourceChart = remoteHelmChart.ChartName
		testSpec.rootCommitFn = helmChartVersion(remoteHelmChart.ChartName + ":" + testSpec.sourceVersion)
	}

	// Reuse the RootSync instead of creating a new one so that testing resources can be cleaned up after the test.
	nt.T.Logf("Update RootSync to sync %s from repo %s", tenant, testSpec.sourceRepo)
	switch testSpec.sourceType {
	case v1beta1.GitSource:
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "secretRef": {"name": ""}}}}`,
			tenant, syncBranch, testSpec.sourceRepo, testSpec.gsaEmail))
	case v1beta1.OciSource:
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s"}, "git": null}}`,
			v1beta1.OciSource, tenant, testSpec.sourceRepo, testSpec.gsaEmail))
	case v1beta1.HelmSource:
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"chart": "%s", "repo": "%s", "version": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "releaseName": "my-coredns", "namespace": "coredns"}, "git": null}}`,
			v1beta1.HelmSource, testSpec.sourceChart, testSpec.sourceRepo, testSpec.sourceVersion, testSpec.gsaEmail))
	}

	if testSpec.fleetWITest {
		nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
			return validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationExists)
		})
	}
	if testSpec.sourceType == v1beta1.HelmSource {
		err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(testSpec.rootCommitFn),
			nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: testSpec.sourceChart}))
		if err != nil {
			nt.T.Fatal(err)
		}
		if err := nt.Validate(fmt.Sprintf("my-coredns-%s", testSpec.sourceChart), "coredns", &appsv1.Deployment{}); err != nil {
			nt.T.Error(err)
		}
	} else {
		err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(testSpec.rootCommitFn),
			nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
		if err != nil {
			nt.T.Fatal(err)
		}
		validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
	}
}

// clusterDescribe represents the output format of gcloud container clusters describe
// this struct contains the field we are interested in
type clusterDescribe struct {
	// WorkloadIdentityConfig is the workload identity config
	WorkloadIdentityConfig workloadIdentityConfig `json:"workloadIdentityConfig"`
}

type workloadIdentityConfig struct {
	// WorkloadPool is the workload pool
	WorkloadPool string `json:"workloadPool"`
}

// getWorkloadPool verifies that the target cluster has workload identity enabled
func getWorkloadPool(nt *nomostest.NT) (string, error) {
	args := []string{
		"container", "clusters", "describe", nt.ClusterName,
		"--project", *e2e.GCPProject,
		"--format", "json",
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	cmd := nt.Shell.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	clusterConfig := &clusterDescribe{}
	if err := json.Unmarshal(out, clusterConfig); err != nil {
		return "", err
	}
	return clusterConfig.WorkloadIdentityConfig.WorkloadPool, nil
}

// validateFWICredentials validates whether the reconciler Pod manifests includes
// the FWI credentials annotation or not.
func validateFWICredentials(nt *nomostest.NT, reconcilerName string, validationFn func(pod corev1.Pod) error) error {
	var podList = &corev1.PodList{}
	if err := nt.KubeClient.List(podList, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{metadata.ReconcilerLabel: reconcilerName}); err != nil {
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

// deleteMembership deletes the membership from the cluster.
func deleteMembership(fleetMembership, gcpProject string) error {
	out, err := exec.Command("gcloud", "container", "hub", "memberships", "delete", fleetMembership, "--quiet", "--project", gcpProject).CombinedOutput()
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
