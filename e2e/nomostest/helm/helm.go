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
	"strings"

	"github.com/ettle/strcase"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"sigs.k8s.io/kustomize/kyaml/copyutil"
	"sigs.k8s.io/yaml"
)

// RemoteHelmChart represents a remote OCI-based helm chart
type RemoteHelmChart struct {
	// Shell is a helper utility to execute shell commands in a test.
	Shell *testshell.TestShell

	// Logger to write logs to
	Logger *testlogger.TestLogger

	// Project in which to store the chart image
	Project string

	// Location to store the chart image
	Location string

	// RepositoryName in which to store the chart imageS
	RepositoryName string

	// ChartName is the name of the helm chart
	ChartName string

	// ChartVersion is the version of the helm chart
	ChartVersion string

	// LocalChartPath is a local directory from which RemoteHelmChart will read, package, and push the chart from
	LocalChartPath string
}

// CreateRepository uses gcloud to create the repository, if it doesn't exist.
func (r *RemoteHelmChart) CreateRepository() error {
	out, err := r.Shell.ExecWithDebug("gcloud", "artifacts", "repositories",
		"describe", r.RepositoryName,
		"--location", r.Location,
		"--project", r.Project)
	if err != nil {
		if !strings.Contains(string(out), "NOT_FOUND") {
			return fmt.Errorf("failed to describe image repository: %w", err)
		}
		// repository does not exist, continue with creation
	} else {
		// repository already exists, skip creation
		return nil
	}

	r.Logger.Info("Creating image repository")
	_, err = r.Shell.ExecWithDebug("gcloud", "artifacts", "repositories",
		"create", r.RepositoryName,
		"--repository-format", "docker",
		"--location", r.Location,
		"--project", r.Project)
	if err != nil {
		return fmt.Errorf("failed to create image repository: %w", err)
	}
	return nil
}

// RegistryLogin will log into the registry host specified by r.Host using local gcloud credentials
func (r *RemoteHelmChart) RegistryLogin() error {
	var err error
	authCmd := r.Shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := r.Shell.Command("helm", "registry", "login",
		"-uoauth2accesstoken", "--password-stdin",
		fmt.Sprintf("https://%s", r.RegistryHost()))
	loginCmd.Stdin, err = authCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating STDOUT pipe: %w", err)
	}
	if err := loginCmd.Start(); err != nil {
		return fmt.Errorf("starting login command: %w", err)
	}
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("running print-access-token command: %w", err)
	}
	if err := loginCmd.Wait(); err != nil {
		return fmt.Errorf("waiting for login command: %w", err)
	}
	return nil
}

// ConfigureAuthHelper configures the local docker client to use gcloud for
// image registry authorization. Helm uses the docker config.
func (r *RemoteHelmChart) ConfigureAuthHelper() error {
	r.Logger.Info("Updating Docker config to use gcloud for auth")
	if _, err := r.Shell.ExecWithDebug("gcloud", "auth", "configure-docker", r.RegistryHost()); err != nil {
		return fmt.Errorf("failed to configure docker auth: %w", err)
	}
	return nil
}

// RegistryHost returns the domain of the artifact registry
func (r *RemoteHelmChart) RegistryHost() string {
	return fmt.Sprintf("%s-docker.pkg.dev", r.Location)
}

// RepositoryAddress returns the domain and path to the chart repository
func (r *RemoteHelmChart) RepositoryAddress() string {
	return fmt.Sprintf("%s/%s/%s", r.RegistryHost(), r.Project, r.RepositoryName)
}

// RepositoryOCI returns the repository address with the oci:// scheme prefix.
func (r *RemoteHelmChart) RepositoryOCI() string {
	return fmt.Sprintf("oci://%s", r.RepositoryAddress())
}

// ChartAddress returns the domain and path to the chart image
func (r *RemoteHelmChart) ChartAddress() string {
	return fmt.Sprintf("%s/%s", r.RepositoryAddress(), r.ChartName)
}

// CopyChartFromLocal accepts a local path to a helm chart and recursively copies it to r.Dir, modifying
// the name of the copied chart from its original name to r.ChartName
func (r *RemoteHelmChart) CopyChartFromLocal(chartPath string) error {
	r.Logger.Infof("Copying helm chart from test artifacts: %s", chartPath)
	if err := os.MkdirAll(r.LocalChartPath, os.ModePerm); err != nil {
		return fmt.Errorf("creating helm chart directory: %v", err)
	}
	if err := copyutil.CopyDir(chartPath, r.LocalChartPath); err != nil {
		return fmt.Errorf("copying helm chart: %v", err)
	}
	r.Logger.Infof("Updating helm chart name & version: %s:%s", r.ChartName, r.ChartVersion)
	chartFilePath := filepath.Join(r.LocalChartPath, "Chart.yaml")
	err := updateYAMLFile(chartFilePath, func(chartMap map[string]interface{}) error {
		chartMap["name"] = r.ChartName
		chartMap["version"] = r.ChartVersion
		return nil
	})
	if err != nil {
		return fmt.Errorf("updating Chart.yaml: %v", err)
	}
	return nil
}

func updateYAMLFile(name string, updateFn func(map[string]interface{}) error) error {
	chartBytes, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("reading file: %s: %w", name, err)
	}
	var chartManifest map[string]interface{}
	if err := yaml.Unmarshal(chartBytes, &chartManifest); err != nil {
		return fmt.Errorf("parsing yaml file: %s: %w", name, err)
	}
	if err := updateFn(chartManifest); err != nil {
		return fmt.Errorf("updating yaml map for %s: %w", name, err)
	}
	chartBytes, err = yaml.Marshal(chartManifest)
	if err != nil {
		return fmt.Errorf("formatting yaml for %s: %w", name, err)
	}
	if err := os.WriteFile(name, chartBytes, os.ModePerm); err != nil {
		return fmt.Errorf("writing file: %s: %w", name, err)
	}
	return nil
}

// UpdateVersion updates the local version of the helm chart to the specified
// version with a timestamp suffix
func (r *RemoteHelmChart) UpdateVersion(version string) error {
	r.Logger.Infof("Updating helm chart version to %q", version)
	chartFilePath := filepath.Join(r.LocalChartPath, "Chart.yaml")
	err := updateYAMLFile(chartFilePath, func(chartMap map[string]interface{}) error {
		chartMap["version"] = version
		return nil
	})
	if err != nil {
		return fmt.Errorf("updating Chart.yaml: %v", err)
	}
	r.ChartVersion = version
	return nil
}

// Push will package and push the helm chart located at r.Dir to the remote registry.
func (r *RemoteHelmChart) Push() error {
	r.Logger.Infof("Packaging helm chart: %s:%s", r.ChartName, r.ChartVersion)
	parentPath := filepath.Dir(r.LocalChartPath)
	if _, err := r.Shell.Helm("package", r.LocalChartPath, "--destination", parentPath+string(filepath.Separator)); err != nil {
		return fmt.Errorf("packaging helm chart: %w", err)
	}
	r.Logger.Infof("Pushing helm chart: %s:%s", r.ChartName, r.ChartVersion)
	chartFile := filepath.Join(parentPath, fmt.Sprintf("%s-%s.tgz", r.ChartName, r.ChartVersion))
	if _, err := r.Shell.Helm("push", chartFile, r.RepositoryOCI()); err != nil {
		return fmt.Errorf("pushing helm chart: %w", err)
	}
	if err := os.Remove(chartFile); err != nil {
		return fmt.Errorf("deleting local helm chart package: %w", err)
	}
	return nil
}

// Delete the package from the remote registry, including all versions and tags.
func (r *RemoteHelmChart) Delete() error {
	r.Logger.Infof("Deleting helm chart: %s", r.ChartName)
	if _, err := r.Shell.ExecWithDebug("gcloud", "artifacts", "docker", "images", "delete", r.ChartAddress(), "--delete-tags", "--project", r.Project); err != nil {
		return fmt.Errorf("deleting helm chart image from registry: %w", err)
	}
	return nil
}

// PushHelmChart pushes a new helm chart for use during an e2e test.
// Returns a reference to the RemoteHelmChart object and any errors.
func PushHelmChart(nt *nomostest.NT, chartName, chartVersion string) (*RemoteHelmChart, error) {
	if chartName == "" {
		return nil, fmt.Errorf("chart name must not be empty")
	}
	if chartVersion == "" {
		return nil, fmt.Errorf("chart version must not be empty")
	}
	chart := &RemoteHelmChart{
		Shell:    nt.Shell,
		Logger:   nt.Logger,
		Project:  *e2e.GCPProject,
		Location: "us", // store redundantly across regions in the US.
		// Use cluster name to avoid overlap between images in parallel test runs.
		RepositoryName: fmt.Sprintf("config-sync-e2e-test--%s", nt.ClusterName),
		// Use chart name to avoid overlap between multiple charts in the same test.
		LocalChartPath: filepath.Join(nt.TmpDir, chartName),
		// Use test name and timestamp to avoid overlap between sequential test runs.
		ChartName:    generateChartName(chartName, strcase.ToKebab(nt.T.Name())),
		ChartVersion: chartVersion,
	}
	nt.T.Cleanup(func() {
		if err := chart.Delete(); err != nil {
			nt.T.Errorf(err.Error())
		}
	})
	artifactPath := fmt.Sprintf("../testdata/helm-charts/%s", chartName)
	if err := chart.CopyChartFromLocal(artifactPath); err != nil {
		return nil, err
	}
	if err := chart.CreateRepository(); err != nil {
		return nil, err
	}
	// TODO: Figure out why gcloud auth doesn't always work with helm push (401 Unauthorized) on new repositories
	// if err := chart.ConfigureAuthHelper(); err != nil {
	// 	return nil, err
	// }
	if err := chart.RegistryLogin(); err != nil {
		return nil, err
	}
	if err := chart.Push(); err != nil {
		return nil, err
	}
	return chart, nil
}

// creates a chart name from the chart name, test name, and timestamp.
// Result will be no more than 40 characters and can function as a k8s metadata.name.
// Chart name and version must be less than 63 characters combined.
func generateChartName(chartName, testName string) string {
	if len(chartName) > 20 {
		chartName = chartName[:20]
		chartName = strings.Trim(chartName, "-")
	}
	chartName = fmt.Sprintf("%s-%s", chartName, testName)
	if len(chartName) > 40 {
		chartName = chartName[:40]
		chartName = strings.Trim(chartName, "-")
	}
	return chartName
}
