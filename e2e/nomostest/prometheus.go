// Copyright 2023 Google LLC
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

package nomostest

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prometheusNamespace                = "prometheus"
	prometheusServerDeploymentName     = "prometheus-deployment"
	prometheusServerPodName            = "prometheus"
	prometheusServerContainerName      = "prometheus"
	prometheusServerPort               = 9090
	prometheusConfigSyncMetricPrefix   = "config_sync_"
	prometheusDistributionBucketSuffix = "_bucket"
	prometheusDistributionCountSuffix  = "_count"
	prometheusDistributionSumSuffix    = "_sum"
	prometheusQueryTimeout             = 10 * time.Second
)

// prometheusManifestDir is the relative path from the testcases package to the prometheus YAML.
var prometheusManifestDir = filepath.Join(baseDir, "e2e", "testdata", "prometheus")

// installPrometheus installs Prometheus on the test cluster and waits for it
// to be ready.
func installPrometheus(nt *NT) error {
	nt.T.Log("[SETUP] Installing Prometheus")
	objs, err := parsePrometheusManifests(nt)
	if err != nil {
		return err
	}
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.KubeClient.Client.Scheme())
		if err != nil {
			return err
		}
		if err := nt.KubeClient.Apply(obj); err != nil {
			return err
		}
		tg.Go(func() error {
			return WatchForCurrentStatus(nt, gvk, nn.Name, nn.Namespace)
		})
	}
	return tg.Wait()
}

// uninstallPrometheus uninstalls Prometheus on the test cluster and waits for
// it to be ready.
func uninstallPrometheus(nt *NT) error {
	nt.T.Log("[CLEANUP] Uninstalling Prometheus")
	objs, err := parsePrometheusManifests(nt)
	if err != nil {
		return err
	}
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.KubeClient.Client.Scheme())
		if err != nil {
			return err
		}
		if err := nt.KubeClient.Delete(obj); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				continue
			}
			return err
		}
		tg.Go(func() error {
			return WatchForNotFound(nt, gvk, nn.Name, nn.Namespace)
		})
	}
	return tg.Wait()
}

// parsePrometheusManifests reads and parses YAML from
// `<repo>/e2e/testdata/prometheus` into typed objects.
func parsePrometheusManifests(nt *NT) ([]client.Object, error) {
	manifestDir, err := filepath.Abs(prometheusManifestDir)
	if err != nil {
		return nil, err
	}
	objs, err := parseManifestDir(manifestDir)
	if err != nil {
		return nil, err
	}
	objs, err = convertToTypedObjects(nt, objs)
	if err != nil {
		return nil, err
	}
	return objs, nil
}

// portForwardPrometheus forwards the prometheus-server pod.
// Cancel the context to kill the port-forward process.
func portForwardPrometheus(ctx context.Context, nt *NT) (address string, err error) {
	// Retry port-forwarding in case the Deployment is in the process of upgrade.
	took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
		pod, err := nt.KubeClient.GetDeploymentPod(prometheusServerDeploymentName, prometheusNamespace, nt.DefaultWaitTimeout)
		if err != nil {
			return err
		}
		port, err := nt.ForwardToFreePort(ctx, prometheusServerPodName, pod.Name,
			prometheusServerPort)
		if err != nil {
			return err
		}
		address = fmt.Sprintf("localhost:%d", port)
		return nil
	})
	if err != nil {
		return "", err
	}
	nt.T.Logf("took %v to wait for prometheus port-forward", took)
	return address, nil
}
