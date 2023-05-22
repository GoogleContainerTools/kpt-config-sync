// Copyright 2025 Google LLC
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
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestHPA(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Checking if metrics-server is installed")
	// GKE installs metrics-server in the kube-system namespace,
	// but our test fixture installs into the metrics-system namespace,
	// so check both.
	metricsServerList := &appsv1.DeploymentList{}
	nt.Must(nt.KubeClient.List(metricsServerList,
		client.MatchingFields{"metadata.name": "metrics-server"}))

	switch len(metricsServerList.Items) {
	case 0:
		// Install if not found.
		// Normal test cleanup will handle uninstall.
		nt.T.Log("Deploy metrics-server Deployment & APIService")
		nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/metrics-server/components-v0.7.2.yaml", yamlDir), "acme/namespaces/metrics-system/components-v0.7.2.yaml"))
		nt.Must(rootSyncGitRepo.CommitAndPush("Add metrics-server"))
		nt.Must(nt.WatchForAllSyncs())

		nt.T.Log("Wait for metrics-server Deployment to be available")
		nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "metrics-server", "metrics-system"))
	case 1:
		metricsServerNS := metricsServerList.Items[0].Namespace
		nt.T.Logf("Found metrics-server Deployment in namespace %q", metricsServerNS)

		nt.T.Log("Wait for metrics-server Deployment to be available")
		nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "metrics-server", metricsServerNS))
	default:
		for _, obj := range metricsServerList.Items {
			nt.T.Logf("Found metrics-server Deployment in namespace %q", obj.Namespace)
		}
		nt.T.Fatal("Too many metrics-server deployments installed")
	}

	nt.T.Log("Wait for v1beta1.metrics.k8s.io APIService to be available")
	nt.Must(nt.Watcher.WatchObject(kinds.APIService(), "v1beta1.metrics.k8s.io", "",
		testwatcher.WatchPredicates(testpredicates.HasConditionStatus(nt.Scheme,
			string(apiregistrationv1.Available), corev1.ConditionTrue))))

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
			return fmt.Errorf("metrics have changed:\n%s",
				cmp.Diff(oldHPPObj.Spec.Metrics, newHPAObj.Spec.Metrics))
		}
		return errors.New("metrics have not changed")
	}

	// Once available, the spec.metrics should not change, because the hello-app
	// only performs work when requests are made.
	// https://github.com/GoogleCloudPlatform/kubernetes-engine-samples/blob/main/quickstarts/hello-app/main.go
	err := nt.Watcher.WatchObject(kinds.HorizontalPodAutoscaler(), "hello-world", "hello-world",
		testwatcher.WatchPredicates(metricsHaveChanged),
		testwatcher.WatchTimeout(30*time.Second))
	if !errors.Is(err, context.DeadlineExceeded) {
		nt.T.Fatalf("expected DeadlineExceeded error, but got: %v", err)
	}
}
