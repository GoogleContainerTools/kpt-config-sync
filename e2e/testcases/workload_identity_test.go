// Copyright 2024 Google LLC
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
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/artifactregistry"
	"kpt.dev/configsync/e2e/nomostest/iam"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
)

// TestWorkloadIdentity tests both GKE WI and Fleet WI for all source types.
// It has the following requirements:
// 1. GKE cluster with workload identity enabled.
// 2. The provided Google service account exists.
// 3. The cluster is registered to a fleet if testing Fleet WI.
// 4. IAM permission and IAM policy binding are created.
func TestWorkloadIdentity(t *testing.T) {
	testCases := []struct {
		name          string
		fleetWITest   bool
		crossProject  bool
		sourceRepo    string
		sourceChart   string
		sourceVersion string
		sourceType    v1beta1.SourceType
		gsaEmail      string
		rootCommitFn  nomostest.Sha1Func
	}{
		{
			name:         "Authenticate to Git repo on CSR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			sourceRepo:   csrRepo(),
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gsaCSRReaderEmail(),
			rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
		},
		{
			name:         "Authenticate to Git repo on CSR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			sourceRepo:   csrRepo(),
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gsaCSRReaderEmail(),
			rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
		},
		{
			name:         "Authenticate to Git repo on CSR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			sourceRepo:   csrRepo(),
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gsaCSRReaderEmail(),
			rootCommitFn: nomostest.RemoteRootRepoSha1Fn,
		},
		{
			name:         "Authenticate to OCI image on AR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			sourceRepo:   privateARImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaARReaderEmail(),
			rootCommitFn: imageDigestFunc(privateARImage()),
		},
		{
			name:         "Authenticate to OCI image on GCR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			sourceRepo:   privateGCRImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaGCRReaderEmail(),
			rootCommitFn: imageDigestFunc(privateGCRImage()),
		},
		{
			name:         "Authenticate to OCI image on AR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			sourceRepo:   privateARImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaARReaderEmail(),
			rootCommitFn: imageDigestFunc(privateARImage()),
		},
		{
			name:         "Authenticate to OCI image on GCR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			sourceRepo:   privateGCRImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaGCRReaderEmail(),
			rootCommitFn: imageDigestFunc(privateGCRImage()),
		},
		{
			name:         "Authenticate to OCI image on AR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			sourceRepo:   privateARImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaARReaderEmail(),
			rootCommitFn: imageDigestFunc(privateARImage()),
		},
		{
			name:         "Authenticate to OCI image on GCR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			sourceRepo:   privateGCRImage(),
			sourceType:   v1beta1.OciSource,
			gsaEmail:     gsaGCRReaderEmail(),
			rootCommitFn: imageDigestFunc(privateGCRImage()),
		},
		{
			name:          "Authenticate to Helm chart on AR with GKE WI",
			fleetWITest:   false,
			crossProject:  false,
			sourceVersion: privateCoreDNSHelmChartVersion,
			sourceChart:   privateCoreDNSHelmChart,
			sourceType:    v1beta1.HelmSource,
			gsaEmail:      gsaARReaderEmail(),
			rootCommitFn:  nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion),
		},
		{
			name:          "Authenticate to Helm chart on AR with Fleet WI in the same project",
			fleetWITest:   true,
			crossProject:  false,
			sourceVersion: privateCoreDNSHelmChartVersion,
			sourceChart:   privateCoreDNSHelmChart,
			sourceType:    v1beta1.HelmSource,
			gsaEmail:      gsaARReaderEmail(),
			rootCommitFn:  nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion),
		},
		{
			name:          "Authenticate to Helm chart on AR with Fleet WI across project",
			fleetWITest:   true,
			crossProject:  true,
			sourceVersion: privateCoreDNSHelmChartVersion,
			sourceChart:   privateCoreDNSHelmChart,
			sourceType:    v1beta1.HelmSource,
			gsaEmail:      gsaARReaderEmail(),
			rootCommitFn:  nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.Unstructured, ntopts.RequireGKE(t))
			if err := workloadidentity.ValidateEnabled(nt); err != nil {
				nt.T.Fatal(err)
			}

			if err := iam.ValidateServiceAccountExists(nt, tc.gsaEmail); err != nil {
				nt.T.Fatal(err)
			}

			// Truncate the fleetMembership length to be at most 63 characters.
			fleetMembership := truncateStringByLength(fmt.Sprintf("%s-%s", truncateStringByLength(*e2e.GCPProject, 20), nt.ClusterName), 63)
			gkeURI := "https://container.googleapis.com/v1/projects/" + *e2e.GCPProject
			if *e2e.GCPRegion != "" {
				gkeURI += fmt.Sprintf("/locations/%s/clusters/%s", *e2e.GCPRegion, nt.ClusterName)
			} else {
				gkeURI += fmt.Sprintf("/zones/%s/clusters/%s", *e2e.GCPZone, nt.ClusterName)
			}

			testutils.ClearMembershipInfo(nt, fleetMembership, *e2e.GCPProject, gkeURI)
			testutils.ClearMembershipInfo(nt, fleetMembership, testutils.TestCrossProjectFleetProjectID, gkeURI)

			rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
			nt.T.Cleanup(func() {
				testutils.ClearMembershipInfo(nt, fleetMembership, *e2e.GCPProject, gkeURI)
				testutils.ClearMembershipInfo(nt, fleetMembership, testutils.TestCrossProjectFleetProjectID, gkeURI)
			})

			tenant := "tenant-a"

			// Register the cluster for fleet workload identity test
			if tc.fleetWITest {
				fleetProject := *e2e.GCPProject
				if tc.crossProject {
					fleetProject = testutils.TestCrossProjectFleetProjectID
				}
				nt.T.Logf("Register the cluster to a fleet in project %q", fleetProject)
				if err := testutils.RegisterCluster(nt, fleetMembership, fleetProject, gkeURI); err != nil {
					nt.T.Fatalf("Failed to register the cluster to project %q: %v", fleetProject, err)
					exists, err := testutils.FleetHasMembership(nt, fleetMembership, fleetProject)
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
			if tc.sourceType == v1beta1.HelmSource {
				chart, err := artifactregistry.PushHelmChart(nt, tc.sourceChart, tc.sourceVersion)
				if err != nil {
					nt.T.Fatalf("failed to push helm chart: %v", err)
				}

				tc.sourceRepo = chart.Image.RepositoryOCI()
				tc.sourceChart = chart.Image.Name
				tc.sourceVersion = chart.Image.Version
				tc.rootCommitFn = nomostest.HelmChartVersionShaFn(chart.Image.Version)
			}

			// Reuse the RootSync instead of creating a new one so that testing resources can be cleaned up after the test.
			nt.T.Logf("Update RootSync to sync %s from repo %s", tenant, tc.sourceRepo)
			switch tc.sourceType {
			case v1beta1.GitSource:
				nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "main", "repo": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "secretRef": {"name": ""}}}}`,
					tenant, tc.sourceRepo, tc.gsaEmail))
			case v1beta1.OciSource:
				nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s"}, "git": null}}`,
					v1beta1.OciSource, tenant, tc.sourceRepo, tc.gsaEmail))
			case v1beta1.HelmSource:
				nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"chart": "%s", "repo": "%s", "version": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "releaseName": "my-coredns", "namespace": "coredns"}, "git": null}}`,
					v1beta1.HelmSource, tc.sourceChart, tc.sourceRepo, tc.sourceVersion, tc.gsaEmail))
			}

			if tc.fleetWITest {
				nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
					return testutils.ReconcilerPodHasFWICredsAnnotation(nt, nomostest.DefaultRootReconcilerName, tc.gsaEmail)
				})
			}
			if tc.sourceType == v1beta1.HelmSource {
				err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(tc.rootCommitFn),
					nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tc.sourceChart}))
				if err != nil {
					nt.T.Fatal(err)
				}
				if err := nt.Validate(fmt.Sprintf("my-coredns-%s", tc.sourceChart), "coredns", &appsv1.Deployment{}); err != nil {
					nt.T.Error(err)
				}
			} else {
				err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(tc.rootCommitFn),
					nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
				if err != nil {
					nt.T.Fatal(err)
				}
				kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
			}
		})
	}
}

func truncateStringByLength(s string, l int) string {
	if len(s) > l {
		return s[:l]
	}
	return s
}
