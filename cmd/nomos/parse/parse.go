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

package parse

import (
	"context"
	"time"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const timeout = time.Second * 15

// GetSyncedCRDs returns the CRDs synced to the cluster in the current context.
//
// Times out after 15 seconds.
func GetSyncedCRDs(ctx context.Context, skipAPIServer bool) ([]*v1beta1.CustomResourceDefinition, status.MultiError) {
	if skipAPIServer {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	config, err := restconfig.MonoRepoRestClient(restconfig.DefaultTimeout)
	if err != nil {
		return nil, getSyncedCRDsError(err, "failed to create rest config")
	}

	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, getSyncedCRDsError(err, "failed to create mapper")
	}

	s := runtime.NewScheme()
	if sErr := v1.AddToScheme(s); sErr != nil {
		return nil, getSyncedCRDsError(sErr, "could not add configmanagement types to scheme")
	}
	if sErr := configsyncv1beta1.AddToScheme(s); sErr != nil {
		return nil, getSyncedCRDsError(sErr, "could not add configsync types to scheme")
	}
	c, cErr := client.New(config, client.Options{
		Scheme: s,
		Mapper: mapper,
	})
	if cErr != nil {
		return nil, getSyncedCRDsError(cErr, "failed to create client")
	}
	configs := &namespaceconfig.AllConfigs{}
	decorateErr := namespaceconfig.DecorateWithClusterConfigs(ctx, c, configs)
	if decorateErr != nil {
		return nil, decorateErr
	}

	decoder := decode.NewGenericResourceDecoder(scheme.Scheme)
	syncedCRDs, crdErr := clusterconfig.GetCRDs(decoder, configs.ClusterConfig)
	if crdErr != nil {
		// We were unable to parse the CRDs from the current ClusterConfig, so bail out.
		// TODO: Make error message more user-friendly when this happens.
		return nil, crdErr
	}
	return syncedCRDs, nil
}

func getSyncedCRDsError(err error, message string) status.Error {
	return status.APIServerError(err, message+". Did you mean to run with --no-api-server-check?")
}
