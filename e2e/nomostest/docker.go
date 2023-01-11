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
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/docker"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/reconcilermanager"
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

// checkImages ensures that all required images are installed on the local
// docker registry.
func checkImages(t testing.NTB) {
	t.Helper()

	var imageNames = []string{
		reconcilermanager.Reconciler,
		reconcilermanager.ManagerName,
	}

	for _, imageName := range imageNames {
		image := imageTagFromManifest(t, imageName)
		checkImage(t, image)
	}
}

// VersionFromManifest parses the image tag from the local manifest
func VersionFromManifest(t testing.NTB) string {
	t.Helper()
	image := imageTagFromManifest(t, reconcilermanager.ManagerName)
	split := strings.Split(image, ":")
	if len(split) < 2 {
		t.Fatalf("unexpected format of image: %s", split)
	}
	return split[len(split)-1]
}

func imageTagFromManifest(t testing.NTB, name string) string {
	bytes, err := os.ReadFile(configSyncManifest)
	if err != nil {
		t.Fatalf("failed to read %s: %s", configSyncManifest, err)
	}
	re := regexp.MustCompile(
		fmt.Sprintf(`(image: )(%s\/%s:.*)`, e2e.DefaultImagePrefix, name),
	)
	match := re.FindStringSubmatch(string(bytes))
	if match == nil || len(match) != 3 {
		t.Fatalf("failed to find image %s in %s", name, configSyncManifest)
	}
	return match[2]
}

func checkImage(t testing.NTB, image string) {
	url := fmt.Sprintf("http://%s", image)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to check for image %s in registry: %s", image, err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response for image %s in registry: %s", image, err)
	}
}
