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

package nomostest

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest/docker"
)

func connectToLocalRegistry(nt *NT) {
	nt.T.Helper()
	docker.StartLocalRegistry(nt.T)

	// We get access to the kubectl API before the Kind cluster is finished being
	// set up, so the control plane is sometimes still being modified when we do
	// this.
	_, err := Retry(20*time.Second, func() error {
		// See https://kind.sigs.k8s.io/docs/user/local-registry/ for explanation.
		node := &corev1.Node{}
		err := nt.Get(nt.ClusterName+"-control-plane", "", node)
		if err != nil {
			return err
		}
		node.Annotations["kind.x-k8s.io/registry"] = fmt.Sprintf("localhost:%d", docker.RegistryPort)
		return nt.Update(node)
	})
	if err != nil {
		nt.T.Fatalf("connecting cluster to local Docker registry: %v", err)
	}
}
