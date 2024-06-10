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

package migrate

import (
	"context"
	"fmt"

	"kpt.dev/configsync/cmd/nomos/status"
)

func migrateMonoRepo(ctx context.Context, cc *status.ClusterClient, kubeCtx string) error {
	isMulti, err := cc.ConfigManagement.IsMultiRepo(ctx)
	if err != nil {
		return err
	}
	if isMulti != nil && *isMulti {
		// already using multi-repo
		printNotice("The cluster is already running in the multi-repo mode. No RootSync will be created.")
		return nil
	}

	fmt.Printf("Enabling the multi-repo mode on cluster %q ...\n", kubeCtx)
	rootSync, rsYamlFile, err := saveRootSyncYAML(ctx, cc.ConfigManagement, kubeCtx)
	if err != nil {
		return err
	}
	cm, cmYamlFile, err := saveConfigManagementYAML(ctx, cc.ConfigManagement, kubeCtx)
	if err != nil {
		return err
	}
	printHint(`Resources for the multi-repo mode have been saved in a temp folder. If the migration process is terminated, it can be recovered manually by running the following commands:
  kubectl apply -f %s && \
  kubectl wait --for condition=established crd rootsyncs.configsync.gke.io && \
  kubectl apply -f %s`, cmYamlFile, rsYamlFile)

	if dryRun {
		dryrun()
		return nil
	}
	if err := executeMonoRepoMigration(ctx, cc, cm, rootSync); err != nil {
		return err
	}
	return nil
}
