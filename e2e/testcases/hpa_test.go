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
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestHPA(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.Unstructured)

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
		nt.Must(nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/metrics-server/components-v0.6.3.yaml", yamlDir), "acme/namespaces/metrics-system/components-v0.6.3.yaml"))
		nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add metrics-server"))
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
		nt.T.Log("Wait for metrics-server Deployment to be available")
		if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "metrics-server", "metrics-system"); err != nil {
			nt.T.Fatal(err)
		}
	}

	nt.T.Log("Deploy hello-world Deployment")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/hpa/ns-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/ns-helloworld.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/hpa/deployment-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/deployment-helloworld.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add deployment"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for hello-world Deployment to be available")
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "hello-world", "hello-world"); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Deploy hello-world HPA")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/hpa/hpa-helloworld.yaml", yamlDir), "acme/namespaces/helloworld/hpa-helloworld.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add HPA"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for hello-world HPA to be available")
	if err := nt.Watcher.WatchForCurrentStatus(kinds.HorizontalPodAutoscaler(), "hello-world", "hello-world"); err != nil {
		nt.T.Fatal(err)
	}

	oldHPPObj := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := nt.KubeClient.Get("hello-world", "hello-world", oldHPPObj); err != nil {
		nt.T.Fatal(err)
	}

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
		[]testpredicates.Predicate{metricsHaveChanged},
		testwatcher.WatchTimeout(5*time.Minute))
	if errors.Is(err, context.DeadlineExceeded) {
		// success!
	} else if err != nil {
		nt.T.Fatal(err)
	} else {
		nt.T.Fatal("unexpected result: WatchObject with metricsHaveChanged should always error")
	}
}
