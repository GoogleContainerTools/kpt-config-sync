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

package namespaceconfig

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "kpt.dev/configsync/pkg/api/monorepo/v1"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DecorateWithClusterConfigs updates AllPolices with all the ClusterConfigs from APIServer.
func DecorateWithClusterConfigs(ctx context.Context, reader client.Reader, policies *AllConfigs) status.MultiError {
	clusterConfigs := &v1.ClusterConfigList{}
	if err := reader.List(ctx, clusterConfigs); err != nil {
		if meta.IsNoMatchError(err) {
			// This can only happen if the ClusterConfig types does not exist on the
			// cluster. This function is only run in "nomos vet" or in mono-repo mode,
			// so this branch can only be validly reached if a user runs "nomos vet"
			// and is connected to a cluster which does not have ACM installed.
			//
			// We don't care to handle the case where a user is running ACM in
			// mono-repo mode and has removed both the Operator and the ClusterConfig
			// CRD.
			return nil
		}
		return status.APIServerError(err, "failed to list ClusterConfigs")
	}

	for _, c := range clusterConfigs.Items {
		switch n := c.Name; n {
		case v1.ClusterConfigName:
			policies.ClusterConfig = c.DeepCopy()
		case v1.CRDClusterConfigName:
			policies.CRDClusterConfig = c.DeepCopy()
		default:
			return status.UndocumentedErrorf("found an invalid ClusterConfig: %s", n)
		}
	}
	return nil
}
