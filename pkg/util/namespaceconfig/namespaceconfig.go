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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListConfigs returns all configs from API server.
func ListConfigs(ctx context.Context, cache cache.Cache) (*AllConfigs, error) {
	configs := AllConfigs{
		NamespaceConfigs: make(map[string]v1.NamespaceConfig),
	}

	// NamespaceConfigs
	namespaceConfigs := &v1.NamespaceConfigList{}
	if err := cache.List(ctx, namespaceConfigs); err != nil {
		return nil, errors.Wrap(err, "failed to list NamespaceConfigs")
	}
	for _, n := range namespaceConfigs.Items {
		configs.NamespaceConfigs[n.Name] = *n.DeepCopy()
	}

	// ClusterConfigs
	if cErr := DecorateWithClusterConfigs(ctx, cache, &configs); cErr != nil {
		return nil, errors.Wrap(cErr, "failed to list ClusterConfigs")
	}

	// Syncs
	var err error
	configs.Syncs, err = listSyncs(ctx, cache)
	return &configs, err
}

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

// listSyncs gets a map-by-name of Syncs currently present in the cluster from
// the cache.
func listSyncs(ctx context.Context, cache cache.Cache) (map[string]v1.Sync, error) {
	syncs := &v1.SyncList{}
	if err := cache.List(ctx, syncs); err != nil {
		return nil, errors.Wrap(err, "failed to list Syncs")
	}

	ret := make(map[string]v1.Sync, len(syncs.Items))
	for _, s := range syncs.Items {
		ret[s.Name] = *s.DeepCopy()
	}
	return ret, nil
}
