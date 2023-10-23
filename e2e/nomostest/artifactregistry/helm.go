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

package artifactregistry

import (
	"fmt"
	"os"
	"path/filepath"

	"kpt.dev/configsync/e2e/nomostest"
	"sigs.k8s.io/yaml"
)

// SetupHelmChart creates a new HelmChart for use during an e2e test.
// Returns a reference to the Image object and any errors.
func SetupHelmChart(nt *nomostest.NT, name, version string) (*HelmChart, error) {
	image, err := SetupImage(nt, name, version)
	if err != nil {
		return nil, err
	}
	helmChart := &HelmChart{
		Image: image,
	}
	if err := helmChart.RegistryLogin(); err != nil {
		return nil, err
	}
	return helmChart, nil
}

// PushHelmChart pushes a new helm chart for use during an e2e test.
// Returns a reference to the RemoteHelmChart object and any errors.
func PushHelmChart(nt *nomostest.NT, name, version string) (*HelmChart, error) {
	chart, err := SetupHelmChart(nt, name, version)
	if err != nil {
		return nil, err
	}
	artifactPath := fmt.Sprintf("../testdata/helm-charts/%s", name)
	if err := chart.CopyLocalPackage(artifactPath); err != nil {
		return nil, err
	}
	if err := chart.Push(); err != nil {
		return nil, err
	}
	return chart, nil
}

// HelmChart represents a remote OCI-based helm chart
type HelmChart struct {
	Image *Image
}

// RegistryLogin will log into the registry with helm using a gcloud auth token.
func (r *HelmChart) RegistryLogin() error {
	var err error
	authCmd := r.Image.Shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := r.Image.Shell.Command("helm", "registry", "login",
		"-u", "oauth2accesstoken", "--password-stdin",
		r.Image.RegistryHost())
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

// CopyLocalPackage accepts a local path to a helm chart and recursively copies
// it to r.BuildPath, modifying the name of the copied chart from its original
// name to r.Name and version to r.Version.
func (r *HelmChart) CopyLocalPackage(chartPath string) error {
	if err := r.Image.CopyLocalPackage(chartPath); err != nil {
		return fmt.Errorf("copying helm chart: %v", err)
	}

	r.Image.Logger.Infof("Updating helm chart name & version: %s:%s", r.Image.Name, r.Image.Version)
	chartFilePath := filepath.Join(r.Image.BuildPath, "Chart.yaml")
	err := updateYAMLFile(chartFilePath, func(chartMap map[string]interface{}) error {
		chartMap["name"] = r.Image.Name
		chartMap["version"] = r.Image.Version
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

// SetVersion updates the local version of the helm chart to the specified
// version with a random suffix
func (r *HelmChart) SetVersion(version string) error {
	r.Image.SetVersion(version)
	version = r.Image.Version
	chartFilePath := filepath.Join(r.Image.BuildPath, "Chart.yaml")
	err := updateYAMLFile(chartFilePath, func(chartMap map[string]interface{}) error {
		chartMap["version"] = version
		return nil
	})
	if err != nil {
		return fmt.Errorf("updating Chart.yaml: %v", err)
	}
	return nil
}

// Push will package and push the helm chart located at r.BuildPath to the remote registry.
// Use helm to push, instead of crane, to better simulate the user workflow.
func (r *HelmChart) Push() error {
	r.Image.Logger.Infof("Packaging helm chart: %s:%s", r.Image.Name, r.Image.Version)
	parentPath := filepath.Dir(r.Image.BuildPath)
	if _, err := r.Image.Shell.Helm("package", r.Image.BuildPath, "--destination", parentPath+string(filepath.Separator)); err != nil {
		return fmt.Errorf("packaging helm chart: %w", err)
	}
	r.Image.Logger.Infof("Pushing helm chart: %s:%s", r.Image.Name, r.Image.Version)
	chartFile := filepath.Join(parentPath, fmt.Sprintf("%s-%s.tgz", r.Image.Name, r.Image.Version))
	if _, err := r.Image.Shell.Helm("push", chartFile, r.Image.RepositoryOCI()); err != nil {
		return fmt.Errorf("pushing helm chart: %w", err)
	}
	// Remove local tgz to allow re-push with the same name & version.
	// No need to defer. The whole TmpDir will be deleted in test cleanup.
	if err := os.Remove(chartFile); err != nil {
		return fmt.Errorf("deleting local helm chart package: %w", err)
	}
	return nil
}
