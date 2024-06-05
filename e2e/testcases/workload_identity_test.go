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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/iam"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type sourceConfig struct {
	repo     string
	pkg      string
	chart    string
	version  string
	dir      string
	commitFn nomostest.Sha1Func
}

// TestWorkloadIdentity tests both GKE WI and Fleet WI for all source types.
// It has the following requirements:
// 1. GKE cluster with workload identity enabled.
// 2. The provided Google service account exists.
// 3. The cluster is registered to a fleet if testing Fleet WI.
// 4. IAM permission and IAM policy binding are created.
// 5. The source of truth is hosted on a GCP service, e.g. CSR, GAR, GCR.
func TestWorkloadIdentity(t *testing.T) {
	testCases := []struct {
		name             string
		fleetWITest      bool
		crossProject     bool
		rootSrcCfg       sourceConfig
		nsSrcCfg         sourceConfig
		sourceType       v1beta1.SourceType
		gsaEmail         string
		requireHelmGAR   bool
		requireOCIGAR    bool
		requireCSR       bool
		testKSAMigration bool
		newRootSrcCfg    sourceConfig
		newNSSrcCfg      sourceConfig
	}{
		{
			name:         "Authenticate to Git repo on CSR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			rootSrcCfg:   sourceConfig{pkg: "hydration/kustomize-components", dir: "kustomize-components", commitFn: nomostest.RemoteRootRepoSha1Fn},
			nsSrcCfg:     sourceConfig{pkg: "hydration/namespace-repo", dir: "namespace-repo", commitFn: nomostest.RemoteNsRepoSha1Fn},
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gitproviders.CSRReaderEmail(),
			requireCSR:   true,
		},
		{
			name:         "Authenticate to Git repo on CSR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			rootSrcCfg:   sourceConfig{pkg: "hydration/kustomize-components", dir: "kustomize-components", commitFn: nomostest.RemoteRootRepoSha1Fn},
			nsSrcCfg:     sourceConfig{pkg: "hydration/namespace-repo", dir: "namespace-repo", commitFn: nomostest.RemoteNsRepoSha1Fn},
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gitproviders.CSRReaderEmail(),
			requireCSR:   true,
		},
		{
			name:         "Authenticate to Git repo on CSR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			rootSrcCfg:   sourceConfig{pkg: "hydration/kustomize-components", dir: "kustomize-components", commitFn: nomostest.RemoteRootRepoSha1Fn},
			nsSrcCfg:     sourceConfig{pkg: "hydration/namespace-repo", dir: "namespace-repo", commitFn: nomostest.RemoteNsRepoSha1Fn},
			sourceType:   v1beta1.GitSource,
			gsaEmail:     gitproviders.CSRReaderEmail(),
			requireCSR:   true,
		},
		{
			name:             "Authenticate to OCI image on AR with GKE WI",
			fleetWITest:      false,
			crossProject:     false,
			rootSrcCfg:       sourceConfig{pkg: "hydration/kustomize-components", dir: ".", version: "v1"},
			nsSrcCfg:         sourceConfig{pkg: "hydration/namespace-repo", dir: ".", version: "v1"},
			newRootSrcCfg:    sourceConfig{pkg: "hydration/kustomize-components", dir: "tenant-a", version: "v1"},
			newNSSrcCfg:      sourceConfig{pkg: "hydration/namespace-repo", dir: "test-ns", version: "v1"},
			sourceType:       v1beta1.OciSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireOCIGAR:    true,
		},
		{
			name:         "Authenticate to OCI image on GCR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			rootSrcCfg: sourceConfig{
				repo:     privateGCRImage("kustomize-components"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("kustomize-components"))},
			nsSrcCfg: sourceConfig{
				repo:     privateGCRImage("namespace-repo"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("namespace-repo"))},
			sourceType: v1beta1.OciSource,
			gsaEmail:   gsaGCRReaderEmail(),
		},
		{
			name:             "Authenticate to OCI image on AR with Fleet WI in the same project",
			fleetWITest:      true,
			crossProject:     false,
			rootSrcCfg:       sourceConfig{pkg: "hydration/kustomize-components", dir: ".", version: "v1"},
			nsSrcCfg:         sourceConfig{pkg: "hydration/namespace-repo", dir: ".", version: "v1"},
			newRootSrcCfg:    sourceConfig{pkg: "hydration/kustomize-components", dir: "tenant-a", version: "v1"},
			newNSSrcCfg:      sourceConfig{pkg: "hydration/namespace-repo", dir: "test-ns", version: "v1"},
			sourceType:       v1beta1.OciSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireOCIGAR:    true,
		},
		{
			name:         "Authenticate to OCI image on GCR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			rootSrcCfg: sourceConfig{
				repo:     privateGCRImage("kustomize-components"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("kustomize-components"))},
			nsSrcCfg: sourceConfig{
				repo:     privateGCRImage("namespace-repo"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("namespace-repo"))},
			sourceType: v1beta1.OciSource,
			gsaEmail:   gsaGCRReaderEmail(),
		},
		{
			name:             "Authenticate to OCI image on AR with Fleet WI across project",
			fleetWITest:      true,
			crossProject:     true,
			rootSrcCfg:       sourceConfig{pkg: "hydration/kustomize-components", dir: ".", version: "v1"},
			nsSrcCfg:         sourceConfig{pkg: "hydration/namespace-repo", dir: ".", version: "v1"},
			newRootSrcCfg:    sourceConfig{pkg: "hydration/kustomize-components", dir: "tenant-a", version: "v1"},
			newNSSrcCfg:      sourceConfig{pkg: "hydration/namespace-repo", dir: "test-ns", version: "v1"},
			sourceType:       v1beta1.OciSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireOCIGAR:    true,
		},
		{
			name:         "Authenticate to OCI image on GCR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			rootSrcCfg: sourceConfig{
				repo:     privateGCRImage("kustomize-components"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("kustomize-components"))},
			nsSrcCfg: sourceConfig{
				repo:     privateGCRImage("namespace-repo"),
				dir:      ".",
				commitFn: imageDigestFuncByName(privateGCRImage("namespace-repo"))},
			sourceType: v1beta1.OciSource,
			gsaEmail:   gsaGCRReaderEmail(),
		},
		{
			name:         "Authenticate to Helm chart on AR with GKE WI",
			fleetWITest:  false,
			crossProject: false,
			rootSrcCfg: sourceConfig{
				chart:    privateCoreDNSHelmChart,
				version:  privateCoreDNSHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion)},
			nsSrcCfg: sourceConfig{
				chart:    privateNSHelmChart,
				version:  "0.1.0",
				commitFn: nomostest.HelmChartVersionShaFn("0.1.0")},
			newRootSrcCfg: sourceConfig{
				chart:    privateSimpleHelmChart,
				version:  privateSimpleHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateSimpleHelmChartVersion)},
			newNSSrcCfg: sourceConfig{
				chart:    "simple-ns-chart",
				version:  "1.0.0",
				commitFn: nomostest.HelmChartVersionShaFn("1.0.0")},
			sourceType:       v1beta1.HelmSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireHelmGAR:   true,
		},
		{
			name:         "Authenticate to Helm chart on AR with Fleet WI in the same project",
			fleetWITest:  true,
			crossProject: false,
			rootSrcCfg: sourceConfig{
				chart:    privateCoreDNSHelmChart,
				version:  privateCoreDNSHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion)},
			nsSrcCfg: sourceConfig{
				chart:    privateNSHelmChart,
				version:  "0.1.0",
				commitFn: nomostest.HelmChartVersionShaFn("0.1.0")},
			newRootSrcCfg: sourceConfig{
				chart:    privateSimpleHelmChart,
				version:  privateSimpleHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateSimpleHelmChartVersion)},
			newNSSrcCfg: sourceConfig{
				chart:    "simple-ns-chart",
				version:  "1.0.0",
				commitFn: nomostest.HelmChartVersionShaFn("1.0.0")},
			sourceType:       v1beta1.HelmSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireHelmGAR:   true,
		},
		{
			name:         "Authenticate to Helm chart on AR with Fleet WI across project",
			fleetWITest:  true,
			crossProject: true,
			rootSrcCfg: sourceConfig{
				chart:    privateCoreDNSHelmChart,
				version:  privateCoreDNSHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateCoreDNSHelmChartVersion)},
			nsSrcCfg: sourceConfig{
				chart:    privateNSHelmChart,
				version:  "0.1.0",
				commitFn: nomostest.HelmChartVersionShaFn("0.1.0")},
			newRootSrcCfg: sourceConfig{
				chart:    privateSimpleHelmChart,
				version:  privateSimpleHelmChartVersion,
				commitFn: nomostest.HelmChartVersionShaFn(privateSimpleHelmChartVersion)},
			newNSSrcCfg: sourceConfig{
				chart:    "simple-ns-chart",
				version:  "1.0.0",
				commitFn: nomostest.HelmChartVersionShaFn("1.0.0")},
			sourceType:       v1beta1.HelmSource,
			gsaEmail:         gsaARReaderEmail(),
			testKSAMigration: true,
			requireHelmGAR:   true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var err error
			opts := []ntopts.Opt{ntopts.Unstructured, ntopts.RequireGKE(t),
				ntopts.NamespaceRepo(testNs, configsync.RepoSyncName),
				ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
				ntopts.WithDelegatedControl}
			if tc.requireHelmGAR {
				opts = append(opts, ntopts.RequireHelmArtifactRegistry(t))
			}
			if tc.requireOCIGAR {
				opts = append(opts, ntopts.RequireOCIArtifactRegistry(t))
			}
			if tc.requireCSR {
				opts = append(opts, ntopts.RequireCloudSourceRepository(t))
			}
			nt := nomostest.New(t, nomostesting.WorkloadIdentity, opts...)
			if err = workloadidentity.ValidateEnabled(nt); err != nil {
				nt.T.Fatal(err)
			}

			if err = iam.ValidateServiceAccountExists(nt, tc.gsaEmail); err != nil {
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

			rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
			repoSync := fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName)
			rsRef := nomostest.RootSyncNN(rootSync.Name)
			nsRef := nomostest.RepoSyncNN(repoSync.Namespace, repoSync.Name)
			nt.T.Cleanup(func() {
				testutils.ClearMembershipInfo(nt, fleetMembership, *e2e.GCPProject, gkeURI)
				testutils.ClearMembershipInfo(nt, fleetMembership, testutils.TestCrossProjectFleetProjectID, gkeURI)
			})

			// Register the cluster for fleet workload identity test
			if tc.fleetWITest {
				fleetProject := *e2e.GCPProject
				if tc.crossProject {
					fleetProject = testutils.TestCrossProjectFleetProjectID
				}
				nt.T.Logf("Register the cluster to a fleet in project %q", fleetProject)
				if err = testutils.RegisterCluster(nt, fleetMembership, fleetProject, gkeURI); err != nil {
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

			var rootMeta, nsMeta rsyncValidateMeta
			var rootChart, nsChart *registryproviders.HelmPackage
			nt.T.Logf("Update RootSync and RepoSync to sync from %s", tc.sourceType)
			switch tc.sourceType {
			case v1beta1.GitSource:
				rootMeta = updateRSyncWithGitSourceConfig(nt, rootSync, nt.RootRepos[configsync.RootSyncName], tc.rootSrcCfg)
				nsMeta = updateRSyncWithGitSourceConfig(nt, repoSync, nt.NonRootRepos[nsRef], tc.nsSrcCfg)
			case v1beta1.HelmSource:
				rootChart, err = updateRootSyncWithHelmSourceConfig(nt, rsRef, tc.rootSrcCfg)
				if err != nil {
					nt.T.Fatal(err)
				}
				nsChart, err = updateRepoSyncWithHelmSourceConfig(nt, nsRef, tc.nsSrcCfg)
				if err != nil {
					nt.T.Fatal(err)
				}

			case v1beta1.OciSource:
				if tc.requireOCIGAR { // OCI provider is AR
					rootMeta, err = updateRootSyncWithOCISourceConfig(nt, rsRef, tc.rootSrcCfg)
					if err != nil {
						nt.T.Fatal(err)
					}
					nsMeta, err = updateRepoSyncWithOCISourceConfig(nt, nsRef, tc.nsSrcCfg)
					if err != nil {
						nt.T.Fatal(err)
					}
				} else { // OCI provider is GCR
					nt.MustMergePatch(rootSync, fmt.Sprintf(`{
						"spec": {
							"sourceType": "%s",
							"oci": {
								"image": "%s",
								"dir": "%s",
								"auth": "gcpserviceaccount",
								"gcpServiceAccountEmail": "%s"
							}
						}
					}`, v1beta1.OciSource, tc.rootSrcCfg.repo, tc.rootSrcCfg.dir, tc.gsaEmail))
					rootMeta = rsyncValidateMeta{
						rsRef:    rsRef,
						sha1Func: tc.rootSrcCfg.commitFn,
						syncDir:  tc.rootSrcCfg.dir,
					}
					nt.MustMergePatch(repoSync, fmt.Sprintf(`{
						"spec": {
							"sourceType": "%s",
							"oci": {
								"image": "%s",
								"dir": "%s",
								"auth": "gcpserviceaccount",
								"gcpServiceAccountEmail": "%s"
							}
						}
					}`, v1beta1.OciSource, tc.nsSrcCfg.repo, tc.nsSrcCfg.dir, tc.gsaEmail))
					nsMeta = rsyncValidateMeta{
						rsRef:    nsRef,
						sha1Func: tc.nsSrcCfg.commitFn,
						syncDir:  tc.nsSrcCfg.dir,
					}
				}
			}

			rootReconcilerName := core.RootReconcilerName(rootSync.Name)
			nsReconcilerName := core.NsReconcilerName(repoSync.Namespace, repoSync.Name)
			nt.T.Log("Validate the GSA annotation is added to the RSync's service accounts")
			tg := taskgroup.New()
			tg.Go(func() error {
				return nt.Watcher.WatchObject(kinds.ServiceAccount(), rootReconcilerName, configsync.ControllerNamespace, []testpredicates.Predicate{
					testpredicates.HasAnnotation(controllers.GCPSAAnnotationKey, tc.gsaEmail),
				})
			})
			tg.Go(func() error {
				return nt.Watcher.WatchObject(kinds.ServiceAccount(), nsReconcilerName, configsync.ControllerNamespace, []testpredicates.Predicate{
					testpredicates.HasAnnotation(controllers.GCPSAAnnotationKey, tc.gsaEmail),
				})
			})
			if tc.fleetWITest {
				tg.Go(func() error {
					took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
						return testutils.ReconcilerPodHasFWICredsAnnotation(nt, rootReconcilerName, tc.gsaEmail, configsync.AuthGCPServiceAccount)
					})
					if err != nil {
						return fmt.Errorf("failed after %v to wait for FWI credentials to be injected into the new root reconciler Pod: %w", took, err)
					}
					t.Logf("took %v to wait for FWI credentials to be injected into the new root reconciler Pod", took)
					return nil
				})
				tg.Go(func() error {
					took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
						return testutils.ReconcilerPodHasFWICredsAnnotation(nt, nsReconcilerName, tc.gsaEmail, configsync.AuthGCPServiceAccount)
					})
					if err != nil {
						return fmt.Errorf("failed after %v to wait for FWI credentials to be injected into the new namespace reconciler Pod: %w", took, err)
					}
					t.Logf("took %v to wait for FWI credentials to be injected into the new namespace reconciler Pod", took)
					return nil
				})
			}
			if err = tg.Wait(); err != nil {
				nt.T.Fatal(err)
			}

			switch tc.sourceType {
			case v1beta1.GitSource, v1beta1.OciSource:
				if err = nt.WatchForAllSyncs(
					nomostest.WithRootSha1Func(rootMeta.sha1Func),
					nomostest.WithRepoSha1Func(nsMeta.sha1Func),
					nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
						rootMeta.rsRef: rootMeta.syncDir,
						nsMeta.rsRef:   nsMeta.syncDir})); err != nil {
					nt.T.Fatal(err)
				}
				kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")
				kustomizecomponents.ValidateTenant(nt, nsMeta.rsRef.Namespace, "test-ns", "base")

			case v1beta1.HelmSource:
				if err = nt.WatchForAllSyncs(
					nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(rootChart.Version)),
					nomostest.WithRepoSha1Func(nomostest.HelmChartVersionShaFn(nsChart.Version)),
					nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
						rsRef: rootChart.Name,
						nsRef: nsChart.Name})); err != nil {
					nt.T.Fatal(err)
				}
				if err = nt.Validate(truncateStringByLength(fmt.Sprintf("%s-%s", rootChart.Name, rootChart.Name), 63),
					"default", &appsv1.Deployment{},
					testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rsRef.Name)); err != nil {
					nt.T.Fatal(err)
				}
				if err = nt.Validate(truncateStringByLength(nsChart.Name, 63),
					nsRef.Namespace, &appsv1.Deployment{},
					testpredicates.IsManagedBy(nt.Scheme, declared.Scope(nsRef.Namespace), nsRef.Name)); err != nil {
					nt.T.Fatal(err)
				}
			}

			// Migrate from gcpserviceaccount to k8sserviceaccount
			if tc.testKSAMigration {
				if err := migrateFromGSAtoKSA(nt, tc.fleetWITest, tc.sourceType, rsRef, nsRef, tc.newRootSrcCfg, tc.newNSSrcCfg); err != nil {
					nt.T.Fatal(err)
				}
			}
		})
	}
}

type rootSyncMutator func(rs *v1beta1.RootSync)
type repoSyncMutator func(rs *v1beta1.RepoSync)

func updateRootSyncWithHelmSourceConfig(nt *nomostest.NT, rsRef types.NamespacedName, sc sourceConfig, mutators ...rootSyncMutator) (*registryproviders.HelmPackage, error) {
	chart, err := nt.BuildAndPushHelmPackage(rsRef,
		registryproviders.HelmSourceChart(sc.chart),
		registryproviders.HelmChartVersion(sc.version))
	if err != nil {
		return nil, fmt.Errorf("pushing helm chart: %w", err)
	}
	nt.T.Log("Update RootSync to sync from a helm chart")
	rootSyncHelm := nt.RootSyncObjectHelm(configsync.RootSyncName, chart)
	rootSyncHelm.Spec.Helm.ReleaseName = chart.Name
	for _, mutator := range mutators {
		mutator(rootSyncHelm)
	}
	if err = nt.KubeClient.Apply(rootSyncHelm); err != nil {
		return nil, err
	}
	return chart, nil
}

func updateRepoSyncWithHelmSourceConfig(nt *nomostest.NT, rsRef types.NamespacedName, sc sourceConfig, mutators ...repoSyncMutator) (*registryproviders.HelmPackage, error) {
	chart, err := nt.BuildAndPushHelmPackage(rsRef,
		registryproviders.HelmSourceChart(sc.chart),
		registryproviders.HelmChartVersion(sc.version))
	if err != nil {
		return nil, fmt.Errorf("pushing helm chart: %w", err)
	}
	nt.T.Log("Update RepoSync to sync from a helm chart")
	repoSyncHelm := nt.RepoSyncObjectHelm(rsRef, chart)
	repoSyncHelm.Spec.Helm.ReleaseName = chart.Name
	for _, mutator := range mutators {
		mutator(repoSyncHelm)
	}
	if err = nt.KubeClient.Apply(repoSyncHelm); err != nil {
		return nil, err
	}
	return chart, nil
}

func updateRootSyncWithOCISourceConfig(nt *nomostest.NT, rsRef types.NamespacedName, sc sourceConfig, mutators ...rootSyncMutator) (rsyncValidateMeta, error) {
	meta := rsyncValidateMeta{rsRef: rsRef, syncDir: sc.dir}
	image, err := nt.BuildAndPushOCIImage(rsRef,
		registryproviders.ImageSourcePackage(sc.pkg),
		registryproviders.ImageVersion(sc.version))
	if err != nil {
		return meta, fmt.Errorf("pushing oci image: %w", err)
	}
	nt.T.Log("Update RootSync to sync from an OCI image")
	rootSyncOCI := nt.RootSyncObjectOCI(configsync.RootSyncName, image)
	rootSyncOCI.Spec.Oci.Dir = sc.dir
	for _, mutator := range mutators {
		mutator(rootSyncOCI)
	}
	if err = nt.KubeClient.Apply(rootSyncOCI); err != nil {
		return meta, err
	}
	meta.sha1Func = imageDigestFuncByDigest(image.Digest)
	return meta, nil
}

func updateRepoSyncWithOCISourceConfig(nt *nomostest.NT, rsRef types.NamespacedName, sc sourceConfig, mutators ...repoSyncMutator) (rsyncValidateMeta, error) {
	meta := rsyncValidateMeta{rsRef: rsRef, syncDir: sc.dir}
	image, err := nt.BuildAndPushOCIImage(rsRef,
		registryproviders.ImageSourcePackage(sc.pkg),
		registryproviders.ImageVersion(sc.version))
	if err != nil {
		return meta, fmt.Errorf("pushing oci image: %w", err)
	}
	nt.T.Log("Update RepoSync to sync from an OCI image")
	repoSyncOCI := nt.RepoSyncObjectOCI(rsRef, image)
	repoSyncOCI.Spec.Oci.Dir = sc.dir
	for _, mutator := range mutators {
		mutator(repoSyncOCI)
	}
	if err = nt.KubeClient.Apply(repoSyncOCI); err != nil {
		return meta, err
	}
	meta.sha1Func = imageDigestFuncByDigest(image.Digest)
	return meta, nil
}

type rsyncValidateMeta struct {
	rsRef    types.NamespacedName
	sha1Func nomostest.Sha1Func
	syncDir  string
}

func updateRSyncWithGitSourceConfig(nt *nomostest.NT, rs client.Object, repo *gitproviders.Repository, sc sourceConfig) rsyncValidateMeta {
	nt.Must(repo.Copy("../testdata/"+sc.pkg, "."))
	nt.Must(repo.CommitAndPush("add DRY configs to the repository"))
	nt.MustMergePatch(rs, fmt.Sprintf(`{
					"spec": {
						"git": {
							"dir": "%s"
						}
					}
				}`, sc.dir))
	rsRef := client.ObjectKey{Name: rs.GetName(), Namespace: rs.GetNamespace()}
	return rsyncValidateMeta{rsRef: rsRef, sha1Func: sc.commitFn, syncDir: sc.dir}
}

func truncateStringByLength(s string, l int) string {
	if len(s) > l {
		return s[:l]
	}
	return s
}

// migrateFromGSAtoKSA tests the scenario of migrating from impersonating a GSA
// to leveraging KSA+WI (a.k.a, BYOID/Ubermint).
func migrateFromGSAtoKSA(nt *nomostest.NT, fleetWITest bool, sourceType v1beta1.SourceType, rsRef, nsRef types.NamespacedName, rootSC, nsSC sourceConfig) error {
	nt.T.Log("Update RootSync auth type from gcpserviceaccount to k8sserviceaccount")
	var err error
	var rootMeta, nsMeta rsyncValidateMeta
	var rootChart, nsChart *registryproviders.HelmPackage
	// Change the source config to guarantee new resources can be reconciled with k8sserviceaccount
	switch sourceType {
	case v1beta1.HelmSource:
		rootChart, err = updateRootSyncWithHelmSourceConfig(nt, rsRef, rootSC, func(rs *v1beta1.RootSync) {
			rs.Spec.Helm.Auth = configsync.AuthK8sServiceAccount
		})
		if err != nil {
			nt.T.Fatal(err)
		}
		nsChart, err = updateRepoSyncWithHelmSourceConfig(nt, nsRef, nsSC, func(rs *v1beta1.RepoSync) {
			rs.Spec.Helm.Auth = configsync.AuthK8sServiceAccount
		})
		if err != nil {
			nt.T.Fatal(err)
		}
	case v1beta1.OciSource:
		rootMeta, err = updateRootSyncWithOCISourceConfig(nt, rsRef, rootSC, func(rs *v1beta1.RootSync) {
			rs.Spec.Oci.Auth = configsync.AuthK8sServiceAccount
		})
		if err != nil {
			nt.T.Fatal(err)
		}
		nsMeta, err = updateRepoSyncWithOCISourceConfig(nt, nsRef, nsSC, func(rs *v1beta1.RepoSync) {
			rs.Spec.Oci.Auth = configsync.AuthK8sServiceAccount
		})
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	// Validations
	rootReconcilerName := core.RootReconcilerName(rsRef.Name)
	nsReconcilerName := core.NsReconcilerName(nsRef.Namespace, nsRef.Name)
	nt.T.Log("Validate the GSA annotation is removed from the RSync's service accounts")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.ServiceAccount(), rootReconcilerName, configsync.ControllerNamespace, []testpredicates.Predicate{
			testpredicates.MissingAnnotation(controllers.GCPSAAnnotationKey),
		})
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.ServiceAccount(), nsReconcilerName, configsync.ControllerNamespace, []testpredicates.Predicate{
			testpredicates.MissingAnnotation(controllers.GCPSAAnnotationKey),
		})
	})
	if fleetWITest {
		nt.T.Log("Validate the serviceaccount_impersonation_url is absent from the injected FWI credentials")
		tg.Go(func() error {
			took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
				return testutils.ReconcilerPodHasFWICredsAnnotation(nt, rootReconcilerName, "", configsync.AuthK8sServiceAccount)
			})
			if err != nil {
				return fmt.Errorf("failed after %v to wait for FWI credentials to be injected into the new root reconciler Pod: %w", took, err)
			}
			nt.T.Logf("took %v to wait for FWI credentials to be injected into the new root reconciler Pod", took)
			return nil
		})
		tg.Go(func() error {
			took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
				return testutils.ReconcilerPodHasFWICredsAnnotation(nt, nsReconcilerName, "", configsync.AuthK8sServiceAccount)
			})
			if err != nil {
				return fmt.Errorf("failed after %v to wait for FWI credentials to be injected into the new root reconciler Pod: %w", took, err)
			}
			nt.T.Logf("took %v to wait for FWI credentials to be injected into the new root reconciler Pod", took)
			return nil
		})
	}
	if err = tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	switch sourceType {
	case v1beta1.GitSource, v1beta1.OciSource:
		if err = nt.WatchForAllSyncs(
			nomostest.WithRootSha1Func(rootMeta.sha1Func),
			nomostest.WithRepoSha1Func(nsMeta.sha1Func),
			nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
				rootMeta.rsRef: rootMeta.syncDir,
				nsMeta.rsRef:   nsMeta.syncDir})); err != nil {
			nt.T.Fatal(err)
		}
		kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "../base", "tenant-a")
		if err := nt.ValidateNotFound("tenant-b", "", &corev1.Namespace{}); err != nil {
			return err
		}
		if err := nt.ValidateNotFound("tenant-c", "", &corev1.Namespace{}); err != nil {
			return err
		}
		kustomizecomponents.ValidateTenant(nt, nsMeta.rsRef.Namespace, "test-ns", "../base")

	case v1beta1.HelmSource:
		if err = nt.WatchForAllSyncs(
			nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(rootChart.Version)),
			nomostest.WithRepoSha1Func(nomostest.HelmChartVersionShaFn(nsChart.Version)),
			nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
				rsRef: rootChart.Name,
				nsRef: nsChart.Name})); err != nil {
			nt.T.Fatal(err)
		}
		if err = nt.Validate("deploy-ns", "ns", &appsv1.Deployment{},
			testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rsRef.Name)); err != nil {
			nt.T.Fatal(err)
		}
		if err = nt.Validate("deploy-default", "default", &appsv1.Deployment{},
			testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rsRef.Name)); err != nil {
			nt.T.Fatal(err)
		}
		if err = nt.Validate("repo-sync-deployment", testNs, &appsv1.Deployment{},
			testpredicates.IsManagedBy(nt.Scheme, declared.Scope(nsRef.Namespace), nsRef.Name)); err != nil {
			nt.T.Fatal(err)
		}
	}
	return nil
}
