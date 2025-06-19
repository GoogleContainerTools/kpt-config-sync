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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
)

func TestDeclaredFieldsPod(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	podName := "nginx"
	podNs := "bookstore"

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	// We use literal YAML here instead of an object as:
	// 1) If we used a literal struct the protocol field would implicitly be added.
	// 2) It's really annoying to specify this as Unstructureds.
	nt.Must(rootSyncGitRepo.AddFile("acme/pod.yaml", []byte(fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - image: %s
    name: nginx
    ports:
    - containerPort: 80
`, podName, podNs, nomostesting.NginxImage))))
	nt.Must(rootSyncGitRepo.CommitAndPush("add pod missing protocol from port"))
	nt.Must(nt.WatchForAllSyncs())

	err := nt.Validate(podName, podNs, &corev1.Pod{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(rootSyncGitRepo.Remove("acme/pod.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove the pod"))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Watcher.WatchForNotFound(kinds.Pod(), podName, podNs)
	if err != nil {
		nt.T.Fatal(err)
	}
}
