package e2e

import (
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// This test currently requires KinD because MutatingAdmissionPolicy is alpha
// and requires a feature gate.
func TestMutatingAdmissionPolicy(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.RequireKind(t))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	mapFile := filepath.Join(".", "..", "..", "examples", "mutating-admission-policies", "config-sync-node-placement.yaml")
	nt.T.Cleanup(func() {
		nt.Must(nt.Shell.Kubectl("delete", "--ignore-not-found", "-f", mapFile))
	})
	nt.Must(nt.Shell.Kubectl("apply", "-f", mapFile))
	// TODO: is there a way to wait for MutatingAdmissionPolicy readiness? (it doesn't appear so)
	// sleep hack to give time to propagate
	time.Sleep(5 * time.Second)

	// expected nodeAffinity from the example MutatingAdmissionPolicy yaml
	exampleNodeAffinity := &corev1.NodeAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
			{
				Weight: 1,
				Preference: corev1.NodeSelectorTerm{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "another-node-label-key",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"another-node-label-value"},
						},
					},
				},
			},
		},
	}

	// bounce reconciler-manager Pod and verify the nodeAffinity is applied by MAP
	nt.Must(nomostest.ValidatePodByLabel(nt, "app", reconcilermanager.ManagerName,
		testpredicates.HasExactlyNodeAffinity(&corev1.NodeAffinity{})))
	nt.T.Log("Replacing the reconciler-manager Pod to validate nodeAffinity is added")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, false)
	nt.Must(nomostest.ValidatePodByLabel(nt, "app", reconcilermanager.ManagerName,
		testpredicates.HasExactlyNodeAffinity(exampleNodeAffinity)))

	// update the RootSync to trigger a Deployment change, verify new reconciler Pod has nodeAffinity
	rootSync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, nomostest.DefaultRootSyncID.Name)
	rootSync.Spec.Git.Dir = "foo"
	nt.Must(nt.KubeClient.Apply(rootSync))
	nt.Must(rootSyncGitRepo.Add("foo/ns.yaml", k8sobjects.NamespaceObject("test-map-ns")))
	nt.Must(rootSyncGitRepo.CommitAndPush("add foo-ns under foo/ dir"))
	nt.Must(nt.WatchForSync(kinds.RootSyncV1Beta1(), rootSync.Name, configsync.ControllerNamespace,
		&syncsource.GitSyncSource{
			ExpectedCommit:    rootSyncGitRepo.MustHash(t),
			ExpectedDirectory: "foo",
		}))
	nt.Must(nt.Validate("test-map-ns", "", &corev1.Namespace{}))
	nt.Must(nomostest.ValidatePodByLabel(nt, "app", reconcilermanager.Reconciler,
		testpredicates.HasExactlyNodeAffinity(exampleNodeAffinity)))
}
