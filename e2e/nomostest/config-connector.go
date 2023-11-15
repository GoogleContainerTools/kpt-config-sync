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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
)

const (
	// kccSA is the name of the service account used for KCC tests
	kccSA = "kcc-integration"
	// kccProject is the name of the GCP project used for KCC tests
	kccProject = "cs-dev-hub"
)

// setupConfigConnector creates the ConfigConnector CR on the KCC cluster.
// The target project and associated resources (e.g. GSAs) are currently hard
// coded due to lack of provisioning automation. Eventually we may want to make
// this more flexible/repeatable for different environments.
func setupConfigConnector(nt *NT) error {
	gvk := schema.GroupVersionKind{
		Group:   "core.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "ConfigConnector",
	}
	kccObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"mode":                 "cluster",
				"googleServiceAccount": fmt.Sprintf("%s@%s.iam.gserviceaccount.com", kccSA, kccProject),
			},
		},
	}
	kccObj.SetGroupVersionKind(gvk)
	kccObj.SetName(fmt.Sprintf("configconnector.%s", gvk.Group))

	// TODO(sdowell): there are currently issues with cleanup, most likely related to a
	// known issue in cli-utils. This currently only runs on a dedicated KCC cluster,
	// so it shouldn't be too disruptive to skip cleanup of the ConfigConnector CR.
	//nt.T.Cleanup(func() {
	//	if err := nt.KubeClient.Delete(kccObj); err != nil {
	//		nt.T.Error(err)
	//	}
	//})

	if err := nt.KubeClient.Apply(kccObj); err != nil {
		return fmt.Errorf("applying config connector manifest: %w", err)
	}

	return nt.Watcher.WatchForCurrentStatus(gvk, kccObj.GetName(), "",
		testwatcher.WatchUnstructured())
}
