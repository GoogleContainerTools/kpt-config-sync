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

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

// gsaRestrictedEmail returns a GSA email that has no permissions and no IAM bindings as a WI user.
func gsaRestrictedEmail() string {
	return fmt.Sprintf("e2e-test-restricted-user@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// gsaRestrictedImpersonatedEmail returns a GSA email that has no permissions, but has an IAM binding as a WI user.
func gsaRestrictedImpersonatedEmail() string {
	return fmt.Sprintf("e2e-test-restricted-wi-user@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

const (

	// The exact error message returned for GKE WI is:
	// credential refresh failed: auth URL returned status 500, body: \"error retrieveing TokenSource.Token: compute: Received 403 `Unable to generate access token; IAM returned 403 Forbidden: Permission 'iam.serviceAccounts.getAccessToken' denied on resource (or it may not exist).\\nThis error could be caused by a missing IAM policy binding on the target IAM service account.\\nFor more information, refer to the Workload Identity documentation:\\n\\thttps://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#authenticating_to
	// The exact error message returned for Fleet WI is:
	// credential refresh failed: auth URL returned status 500, body: \"error retrieveing TokenSource.Token: oauth2/google: status code 403: {\\n  \\\"error\\\": {\\n    \\\"code\\\": 403,\\n    \\\"message\\\": \\\"Permission 'iam.serviceAccounts.getAccessToken' denied on resource (or it may not exist).\\\",\\n    \\\"status\\\": \\\"PERMISSION_DENIED\\\",\\n    \\\"details\\\": [\\n      {\\n        \\\"@type\\\": \\\"type.googleapis.com/google.rpc.ErrorInfo\\\",\\n        \\\"reason\\\": \\\"IAM_PERMISSION_DENIED\\\",\\n        \\\"domain\\\": \\\"iam.googleapis.com\\\",\\n        \\\"metadata\\\": {\\n          \\\"permission\\\": \\\"iam.serviceAccounts.getAccessToken\\\"\\n        }\\n      }\\n    ]\\n  }\\n}\\n\\n\"
	missingWIBindingError = `Permission 'iam.serviceAccounts.getAccessToken' denied on resource (or it may not exist).`

	// The exact error message returned by CSR is:
	// remote: PERMISSION_DENIED: The caller does not have permission\\nremote: [type.googleapis.com/google.rpc.RequestInfo]\\nremote: request_id: \\\"6a2759876362432c8d3bb63ad4e45a99\\\"\\nfatal: unable to access 'https://source.developers.google.com/p/stolos-dev/r/kustomize-components/
	missingCSRPermissionError = "PERMISSION_DENIED: The caller does not have permission"

	// The exact error message returned by AR for the OCI image is:
	// failed to pull image us-docker.pkg.dev/stolos-dev/config-sync-test-private/kustomize-components:v1: GET https://us-docker.pkg.dev/v2/token?scope=repository%3Astolos-dev%2Fconfig-sync-test-private%2Fkustomize-components%3Apull\u0026service=: DENIED: Permission \"artifactregistry.repositories.downloadArtifacts\" denied on resource \"projects/stolos-dev/locations/us/repositories/config-sync-test-private\" (or it may not exist)
	missingOCIARPermissionError = `DENIED: Permission \"artifactregistry.repositories.downloadArtifacts\" denied on resource`

	// The exact error message returned by AR for the Helm chart is:
	// unexpected error rendering chart, will retry","Err":"rendering helm chart: invoking helm: Error: failed to authorize: failed to fetch oauth token: unexpected status from GET request to https://us-docker.pkg.dev/v2/token?scope=repository%3Astolos-dev%2Fconfig-sync-e2e-test--nanyu-cluster-1%2Fcoredns-test-helm-argke-workload-identit%3Apull\u0026service=us-docker.pkg.dev: 403 Forbidden
	missingHelmARPermissionError = "403 Forbidden"

	// The exact error message for missing GSA is:
	// metadata: GCE metadata \"instance/service-accounts/default/token?scopes=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcloud-platform\" not defined
	missingGSAError = "not defined"
)

func missingGSAEmail() string {
	return fmt.Sprintf("e2e-test-non-existent-user@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

func TestGSAMissingPermissions(t *testing.T) {
	testCases := []struct {
		name           string
		fwiTest        bool
		sourceType     v1beta1.SourceType
		AuthType       configsync.AuthType
		sourceRepo     string
		sourceChart    string
		sourceVersion  string
		gsaEmail       string
		expectedErrMsg string
	}{
		{
			name:           "GSA - Git source missing GSA for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.GitSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     csrRepo(),
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - OCI source missing GSA for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - Helm source missing GSA for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - Git source missing GSA for Fleet WI",
			fwiTest:        false,
			sourceType:     v1beta1.GitSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     csrRepo(),
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - OCI source missing GSA for Fleet WI",
			fwiTest:        false,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - Helm source missing GSA for Fleet WI",
			fwiTest:        false,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       missingGSAEmail(),
			expectedErrMsg: missingGSAError,
		},
		{
			name:           "GSA - Git source missing IAM binding for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.GitSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     csrRepo(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - OCI source missing IAM binding for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - Helm source missing IAM binding for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - Git source missing IAM binding for Fleet WI",
			fwiTest:        true,
			sourceType:     v1beta1.GitSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     csrRepo(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - OCI source missing IAM binding for Fleet WI",
			fwiTest:        true,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - Helm source missing IAM binding for Fleet WI",
			fwiTest:        true,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingWIBindingError,
		},
		{
			name:           "GSA - Git source with IAM binding for GKE WI, but missing reader permission",
			fwiTest:        false,
			sourceType:     v1beta1.GitSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     csrRepo(),
			gsaEmail:       gsaRestrictedImpersonatedEmail(),
			expectedErrMsg: missingCSRPermissionError,
		},
		{
			name:           "GSA - OCI source with IAM binding for GKE WI, but missing reader permission",
			fwiTest:        false,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       gsaRestrictedImpersonatedEmail(),
			expectedErrMsg: missingOCIARPermissionError,
		},
		{
			name:           "GSA - Helm source with IAM binding for GKE WI, but missing reader permission",
			fwiTest:        false,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       gsaRestrictedImpersonatedEmail(),
			expectedErrMsg: missingHelmARPermissionError,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.Unstructured, ntopts.RequireGKE(t))
			rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
			tenant := "tenant-a"

			if err := workloadidentity.ValidateEnabled(nt); err != nil {
				nt.T.Fatal(err)
			}

			mustConfigureMembership(nt, tc.fwiTest, false)

			spec := &sourceSpec{
				sourceType:    tc.sourceType,
				sourceRepo:    tc.sourceRepo,
				sourceChart:   tc.sourceChart,
				sourceVersion: tc.sourceVersion,
				gsaEmail:      tc.gsaEmail,
			}
			mustConfigureRootSync(nt, rs, tenant, spec)
			nt.WaitForRootSyncSourceError(rs.Name, status.SourceErrorCode, tc.expectedErrMsg)
		})
	}

}

func TestKSAMissingReaderPermission(t *testing.T) {
	testCases := []struct {
		name           string
		fwiTest        bool
		sourceType     v1beta1.SourceType
		AuthType       configsync.AuthType
		sourceRepo     string
		sourceChart    string
		sourceVersion  string
		gsaEmail       string
		expectedErrMsg string
	}{
		{
			name:           "WI-KSA - OCI source missing permission for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingOCIARPermissionError,
		},
		{
			name:           "WI-KSA - Helm source missing permission for GKE WI",
			fwiTest:        false,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingHelmARPermissionError,
		},
		{
			name:           "WI-KSA - OCI source missing permission for Fleet WI",
			fwiTest:        true,
			sourceType:     v1beta1.OciSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceRepo:     privateARImage(),
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingOCIARPermissionError,
		},
		{
			name:           "WI-KSA - Helm source missing permission for Fleet WI",
			fwiTest:        true,
			sourceType:     v1beta1.HelmSource,
			AuthType:       configsync.AuthGCPServiceAccount,
			sourceVersion:  privateCoreDNSHelmChartVersion,
			sourceChart:    privateCoreDNSHelmChart,
			gsaEmail:       gsaRestrictedEmail(),
			expectedErrMsg: missingHelmARPermissionError,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			nt := nomostest.New(t, nomostesting.WorkloadIdentity,
				ntopts.Unstructured,
				ntopts.RequireGKE(t))

			if err := workloadidentity.ValidateEnabled(nt); err != nil {
				nt.T.Fatal(err)
			}
			mustConfigureMembership(nt, tc.fwiTest, false)

			// Add another RootSync (rs-byoid) to the `root-sync` Rootsync.
			rs := fake.RootSyncObjectV1Beta1("rs-byoid")
			nomostest.EnableDeletionPropagation(rs)
			syncDir := "tenant"
			rs.Spec.SourceFormat = string(filesystem.SourceFormatUnstructured)
			rs.Spec.SourceType = string(tc.sourceType)
			spec := &sourceSpec{
				sourceType:    tc.sourceType,
				sourceRepo:    tc.sourceRepo,
				sourceChart:   tc.sourceChart,
				sourceVersion: tc.sourceVersion,
				gsaEmail:      tc.gsaEmail,
			}
			if err := pushSource(nt, spec); err != nil {
				nt.T.Fatal(err)
			}
			switch tc.sourceType {
			case v1beta1.OciSource:
				rs.Spec.Oci = &v1beta1.Oci{
					Image: spec.sourceRepo,
					Dir:   syncDir,
					Auth:  configsync.AuthK8sServiceAccount,
				}
			case v1beta1.HelmSource:
				rs.Spec.Helm = &v1beta1.HelmRootSync{
					HelmBase: v1beta1.HelmBase{
						Repo:        spec.sourceRepo,
						Chart:       spec.sourceChart,
						Version:     spec.sourceVersion,
						ReleaseName: "my-coredns",
						Period:      metav1.Duration{},
						Auth:        configsync.AuthK8sServiceAccount,
					},
					Namespace: "coredns",
				}
			}
			nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/rs-byoid.yaml", rs))
			nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a RootSync with restricted k8sserviceaccount"))

			ksaRef := types.NamespacedName{
				Namespace: configsync.ControllerNamespace,
				Name:      core.RootReconcilerName(rs.Name),
			}
			require.NoError(nt.T,
				nt.WatchForSync(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
					nomostest.DefaultRootSha1Fn, nomostest.RootSyncHasStatusSyncCommit, nil),
			)
			require.NoError(nt.T,
				nt.Watcher.WatchObject(kinds.ServiceAccount(), ksaRef.Name, ksaRef.Namespace, []testpredicates.Predicate{
					testpredicates.MissingAnnotation(controllers.GCPSAAnnotationKey),
				}))
			if tc.fwiTest {
				nt.T.Log("Validate the serviceaccount_impersonation_url is absent from the injected FWI credentials")
				nomostest.Wait(nt.T, "wait for FWI credentials to exist", nt.DefaultWaitTimeout, func() error {
					return testutils.ReconcilerPodHasFWICredsAnnotation(nt, core.RootReconcilerName(rs.Name), "", configsync.AuthK8sServiceAccount)
				})
			}

			nt.WaitForRootSyncSourceError(rs.Name, status.SourceErrorCode, tc.expectedErrMsg)
		})
	}

}
