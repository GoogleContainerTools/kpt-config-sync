package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestHPA(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Checking if metrics-server is installed")
	found := false
	for _, ns := range []string{"kube-system", "metrics-system"} {
		metricsServerObj := &appsv1.Deployment{}
		if err := nt.KubeClient.Get("metrics-server", ns, metricsServerObj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			nt.T.Fatal(err)
		}
		found = true
		break
	}

	if !found {
		nt.T.Log("Deploy metrics-server Deployment & APIService")
		nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/metrics-server/components-v0.7.2.yaml", yamlDir), "acme/namespaces/metrics-system/components-v0.7.2.yaml"))
		nt.Must(rootSyncGitRepo.CommitAndPush("Add metrics-server"))
		nt.Must(nt.WatchForAllSyncs())

		nt.T.Log("Wait for metrics-server Deployment to be available")
		nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "metrics-server", "metrics-system"))
	}

	nt.T.Log("Deploy hello-world Deployment")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/hpa/ns-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/ns-helloworld.yaml"))
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/hpa/deployment-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/deployment-helloworld.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add deployment"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Wait for hello-world Deployment to be available")
	nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "hello-world", "hello-world"))

	nt.T.Log("Deploy hello-world HPA")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/hpa/hpa-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/hpa-helloworld.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add HPA"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Wait for hello-world HPA to be available")
	nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.HorizontalPodAutoscaler(), "hello-world", "hello-world"))

	oldHPPObj := &autoscalingv2.HorizontalPodAutoscaler{}
	nt.Must(nt.KubeClient.Get("hello-world", "hello-world", oldHPPObj))

	metricsHaveChanged := func(obj client.Object) error {
		newHPAObj := obj.(*autoscalingv2.HorizontalPodAutoscaler)
		if !equality.Semantic.DeepEqual(oldHPPObj.Spec.Metrics, newHPAObj.Spec.Metrics) {
			return errors.Errorf("metrics have changed:\n%s",
				cmp.Diff(oldHPPObj.Spec.Metrics, newHPAObj.Spec.Metrics))
		}
		return errors.New("metrics have not changed")
	}

	// Once available, the spec.metrics should not change
	err := nt.Watcher.WatchObject(kinds.HorizontalPodAutoscaler(), "hello-world", "hello-world",
		testwatcher.WatchPredicates(metricsHaveChanged),
		testwatcher.WatchTimeout(30*time.Second))
	if err == nil {
		nt.T.Fatal("unexpected result: WatchObject with metricsHaveChanged should always error")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		nt.T.Fatal(err)
	}
}
