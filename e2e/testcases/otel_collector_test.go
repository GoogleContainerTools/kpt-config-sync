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
	"strings"
	"testing"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
)

func TestOtelCollectorDeploymentErrorFree(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.RequireGKE(t))
	nt.T.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs("config-management-monitoring", ocmetrics.OtelCollectorName, "", false)
		}
	})

	// todo may need to restart otel-collector pod

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme", "acme")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("initialize acme directory")
	nomostest.DeletePodByLabel(nt, "app", "opentelemetry", false)
	nt.WaitForRepoSyncs()

	err := CheckOtelCollectorError(nt)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func CheckOtelCollectorError(nt *nomostest.NT) error {
	args := []string{"logs", fmt.Sprintf("deployment/%s", ocmetrics.OtelCollectorName), "-n", ocmetrics.MonitoringNamespace}
	out, err := nt.Kubectl(args...)
	cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
	if err != nil {
		nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
		return err
	}
	entry := strings.Split(string(out), "\n")
	for _, m := range entry {
		if strings.Contains(m, "rpc error") {
			return fmt.Errorf("error found in %s deployment log: %s", ocmetrics.OtelCollectorName, m)
		}
	}
	return nil
}
