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

package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"sigs.k8s.io/kustomize/kyaml/copyutil"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// PrivateARHelmRegistry is the registry URL to the private AR used for testing.
// Cannot be assigned as a global variable due to GCPProject pointer, which gets
// evaluated after initialization.
func PrivateARHelmRegistry() string {
	return fmt.Sprintf("oci://us-docker.pkg.dev/%s/config-sync-test-ar-helm", *e2e.GCPProject)
}

// PrivateARHelmHost is the host name of the private AR used for testing
var PrivateARHelmHost = "https://us-docker.pkg.dev"

// RemoteHelmChart represents a remote OCI-based helm chart
type RemoteHelmChart struct {
	// Shell is a helper utility to execute shell commands in a test.
	Shell *testshell.TestShell

	// Host is the host URL, e.g. https://us-docker.pkg.dev
	Host string

	// Registry is the registry URL, e.g. oci://us-docker.pkg.dev/oss-prow-build-kpt-config-sync/config-sync-test-ar-helm
	Registry string

	// ChartName is the name of the helm chart
	ChartName string

	// ChartVersion is the version of the helm chart
	ChartVersion string

	// Dir is a local directory from which RemoteHelmChart will read, package, and push the chart from
	Dir string
}

// NewRemoteHelmChart creates a RemoteHelmChart
func NewRemoteHelmChart(shell *testshell.TestShell, host, registry, dir, chartName, version string) *RemoteHelmChart {
	return &RemoteHelmChart{
		Shell:        shell,
		Host:         host,
		Registry:     registry,
		Dir:          dir,
		ChartName:    chartName,
		ChartVersion: version,
	}
}

// RegistryLogin will log into the registry host specified by r.Host using local gcloud credentials
func (r *RemoteHelmChart) RegistryLogin() error {
	var err error
	authCmd := r.Shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := r.Shell.Command("helm", "registry", "login", "-uoauth2accesstoken", "--password-stdin", r.Host)
	loginCmd.Stdin, err = authCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to setup command pipe: %v", err)
	}
	if err := loginCmd.Start(); err != nil {
		return fmt.Errorf("failed to start login command: %v", err)
	}
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("failed to run auth command: %v", err)
	}
	if err := loginCmd.Wait(); err != nil {
		return fmt.Errorf("failed to wait for login command: %v", err)
	}
	return nil
}

// CopyChartFromLocal accepts a local path to a helm chart and recursively copies it to r.Dir, modifying
// the name of the copied chart from its original name to r.ChartName
func (r *RemoteHelmChart) CopyChartFromLocal(chartPath, originalChartName string) error {
	if err := copyutil.CopyDir(filesys.MakeFsOnDisk(), chartPath, r.Dir); err != nil {
		return fmt.Errorf("failed to copy helm chart: %v", err)
	}
	if err := findAndReplaceInFile(filepath.Join(r.Dir, "Chart.yaml"), fmt.Sprintf("name: %s", originalChartName), fmt.Sprintf("name: %s", r.ChartName)); err != nil {
		return fmt.Errorf("failed to rename helm chart: %v", err)
	}
	return nil
}

// UpdateVersion updates the local version of the helm chart to version
func (r *RemoteHelmChart) UpdateVersion(version string) error {
	if err := findAndReplaceInFile(filepath.Join(r.Dir, "Chart.yaml"), fmt.Sprintf("version: %s", r.ChartVersion), fmt.Sprintf("version: %s", version)); err != nil {
		return fmt.Errorf("failed to update helm chart version: %v", err)
	}
	r.ChartVersion = version
	return nil
}

// Push will package and push the helm chart located at r.Dir to the remote registry.
func (r *RemoteHelmChart) Push() error {
	if _, err := r.Shell.Helm("package", r.Dir, "--destination", r.Dir); err != nil {
		return fmt.Errorf("failed to package helm chart: %v", err)
	}
	chartFile := filepath.Join(r.Dir, fmt.Sprintf("%s-%s.tgz", r.ChartName, r.ChartVersion))
	if out, err := r.Shell.Helm("push", chartFile, r.Registry); err != nil {
		return fmt.Errorf("failed to run `helm push`: %s; %v", string(out), err)
	}
	return nil
}

// PushHelmChart pushes a new helm chart for use during an e2e test. Returns a reference to the RemoteHelmChart object
// and any errors that are encountered.
func PushHelmChart(nt *nomostest.NT, helmchart, version string) (*RemoteHelmChart, error) {
	nt.T.Log("Push helm chart to the artifact registry")

	chartName := generateChartName(helmchart, nt.ClusterName)
	nt.T.Cleanup(func() {
		if err := cleanHelmImages(nt, chartName); err != nil {
			nt.T.Errorf(err.Error())
		}
	})

	remoteHelmChart := NewRemoteHelmChart(nt.Shell, PrivateARHelmHost, PrivateARHelmRegistry(), nt.TmpDir, chartName, version)
	err := remoteHelmChart.CopyChartFromLocal(fmt.Sprintf("../testdata/helm-charts/%s", helmchart), helmchart)
	if err != nil {
		return nil, err
	}
	if err := remoteHelmChart.RegistryLogin(); err != nil {
		return nil, fmt.Errorf("failed to login to the helm registry: %v", err)
	}
	if err := remoteHelmChart.Push(); err != nil {
		return nil, err
	}

	return remoteHelmChart, nil
}

// creates a chart name from current project id, cluster name, and timestamp
func generateChartName(helmchart, clusterName string) string {
	chartName := fmt.Sprintf("%s-%s-%s", helmchart, clusterName, timestampAsString())
	if len(chartName) > 50 {
		// the chartName + releaseName is used as the metadata.name of resources in the coredns helm chart, so we must trim this down
		// to keep it under the k8s length limit
		chartName = chartName[len(chartName)-50:]
		chartName = strings.Trim(chartName, "-")
	}
	return chartName
}

// removes helm charts created during e2e testing
func cleanHelmImages(nt *nomostest.NT, chartName string) error {
	if out, err := nt.Shell.Command("gcloud", "artifacts", "docker", "images", "delete", fmt.Sprintf("us-docker.pkg.dev/%s/config-sync-test-ar-helm/%s", *e2e.GCPProject, chartName), "--delete-tags").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to cleanup helm chart image from registry: %s; %v", string(out), err)
	}
	return nil
}

// finds and replaces particular text string in a file
func findAndReplaceInFile(path, old, new string) error {
	oldFile, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read file: %v", err)
	}
	err = os.WriteFile(path, []byte(strings.ReplaceAll(string(oldFile), old, new)), 0644)
	if err != nil {
		return fmt.Errorf("could not write to file: %v", err)
	}
	return nil
}

// returns the current unix timestamp as a string
func timestampAsString() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}
