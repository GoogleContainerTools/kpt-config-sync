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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ettle/strcase"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"sigs.k8s.io/kustomize/kyaml/copyutil"
)

// DefaultLocation is the default location in which to host Artifact Registry
// repositories. In this case, `us`, which is a multi-region location.
const DefaultLocation = "us"

// RegistryReaderAccountEmail returns the email of the google service account
// with permission to read from Artifact Registry.
func RegistryReaderAccountEmail() string {
	return fmt.Sprintf("e2e-test-ar-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// SetupImage constructs a new Image for use during an e2e test, along with its
// repository. Image will be deleted when the test ends.
// Returns a reference to the Image object and any errors.
func SetupImage(nt *nomostest.NT, name, version string) (*Image, error) {
	if name == "" {
		return nil, fmt.Errorf("image name must not be empty")
	}

	// Version will error out if it's empty or longer than 20 characters
	if err := validateImageVersion(version); err != nil {
		return nil, err
	}
	chart := &Image{
		Shell:   nt.Shell,
		Logger:  nt.Logger,
		Project: *e2e.GCPProject,
		// Store images redundantly across regions in the US.
		Location: DefaultLocation,
		// Use cluster name to avoid overlap.
		RepositoryName: fmt.Sprintf("config-sync-e2e-test--%s", nt.ClusterName),
		// Use chart name to avoid overlap.
		BuildPath: filepath.Join(nt.TmpDir, name),
		// Use test name to avoid overlap. Truncate to 40 characters.
		Name:    generateImageName(nt, name),
		Version: version,
	}
	nt.T.Cleanup(func() {
		if err := chart.Delete(); err != nil {
			nt.T.Errorf(err.Error())
		}
	})
	if err := chart.CreateRepository(); err != nil {
		return nil, err
	}
	if err := chart.CleanBuildPath(); err != nil {
		return nil, err
	}
	// Setting up gcloud as auth helper for Docker _should_ work with both
	// helm and crane, but in practice, sometimes the auth helper errors
	// or hangs when pushing to a new repository.
	// TODO: Test gcloud auth helper and crane/helm login separately
	// if err := chart.ConfigureAuthHelper(); err != nil {
	// 	return nil, err
	// }
	return chart, nil
}

// Image represents a remote OCI image in Artifact Registry
type Image struct {
	// Shell is a helper utility to execute shell commands in a test.
	Shell *testshell.TestShell

	// Logger to write logs to
	Logger *testlogger.TestLogger

	// Project in which to store the image
	Project string

	// Location to store the image
	Location string

	// RepositoryName in which to store the image
	RepositoryName string

	// name is the name of the image
	Name string

	// version is the version of the image
	Version string

	// BuildPath is a local directory from which Image will read, package, and push the image from
	BuildPath string
}

// RegistryHost returns the domain of the artifact registry
func (r *Image) RegistryHost() string {
	return fmt.Sprintf("%s-docker.pkg.dev", r.Location)
}

// RepositoryAddress returns the domain and path to the chart repository
func (r *Image) RepositoryAddress() string {
	return fmt.Sprintf("%s/%s/%s", r.RegistryHost(), r.Project, r.RepositoryName)
}

// RepositoryOCI returns the repository address with the oci:// scheme prefix.
func (r *Image) RepositoryOCI() string {
	return fmt.Sprintf("oci://%s", r.RepositoryAddress())
}

// Address returns the domain and path to the image
func (r *Image) Address() string {
	return fmt.Sprintf("%s/%s", r.RepositoryAddress(), r.Name)
}

// AddressWithTag returns the domain, path, name, and tag of the image
func (r *Image) AddressWithTag() string {
	return fmt.Sprintf("%s/%s:%s", r.RepositoryAddress(), r.Name, r.Version)
}

// CreateRepository uses gcloud to create the repository, if it doesn't exist.
func (r *Image) CreateRepository() error {
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

// ConfigureAuthHelper configures the local docker client to use gcloud for
// image registry authorization. Helm uses the docker config.
func (r *Image) ConfigureAuthHelper() error {
	r.Logger.Info("Updating Docker config to use gcloud for auth")
	if _, err := r.Shell.ExecWithDebug("gcloud", "auth", "configure-docker", r.RegistryHost()); err != nil {
		return fmt.Errorf("failed to configure docker auth: %w", err)
	}
	return nil
}

// CleanBuildPath creates the r.BuildPath if it doesn't exist and deletes all
// contents if it does.
func (r *Image) CleanBuildPath() error {
	r.Logger.Infof("Cleaning build path: %s", r.BuildPath)
	if _, err := os.Stat(r.BuildPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else {
		if err := os.RemoveAll(r.BuildPath); err != nil {
			return fmt.Errorf("deleting package directory: %v", err)
		}
	}
	if err := os.MkdirAll(r.BuildPath, os.ModePerm); err != nil {
		return fmt.Errorf("creating package directory: %v", err)
	}
	return nil
}

// CopyLocalPackage accepts a local path to a package and recursively copies it
// to r.BuildPath.
func (r *Image) CopyLocalPackage(pkgPath string) error {
	r.Logger.Infof("Copying package from test artifacts: %s", pkgPath)
	if err := copyutil.CopyDir(pkgPath, r.BuildPath); err != nil {
		return fmt.Errorf("copying package directory: %v", err)
	}
	return nil
}

// SetName updates the local name of the image to the specified name with a
// random suffix
func (r *Image) SetName(nt *nomostest.NT, name string) {
	name = generateImageName(nt, name)
	r.Logger.Infof("Updating image name to %q", name)
	r.Name = name
}

// SetVersion updates the local version of the image to the specified tag with a
// random suffix
func (r *Image) SetVersion(version string) error {
	if err := validateImageVersion(version); err != nil {
		return err
	}
	r.Version = version
	return nil
}

// Delete the package from the remote registry, including all versions and tags.
func (r *Image) Delete() error {
	r.Logger.Infof("Deleting image: %s", r.Name)
	if _, err := r.Shell.ExecWithDebug("gcloud", "artifacts", "docker", "images", "delete", r.Address(), "--delete-tags", "--project", r.Project); err != nil {
		return fmt.Errorf("deleting image from registry: %w", err)
	}
	return nil
}

// creates a chart name from the chart name, test name, and timestamp.
// Result will be no more than 40 characters and can function as a k8s metadata.name.
// Chart name and version must be less than 63 characters combined.
func generateImageName(nt *nomostest.NT, chartName string) string {
	testName := strcase.ToKebab(nt.T.Name())
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

// validateImageVersion will validate if chart version string is not empty and
// is 20 characters maximum and can function as a k8s metadata.name.
// Chart name and version must be less than 63 characters combined.
func validateImageVersion(chartVersion string) error {
	if chartVersion == "" {
		return fmt.Errorf("image version must not be empty")
	}
	if len(chartVersion) > 20 {
		return fmt.Errorf("chart version string %q should not exceed 20 characters", chartVersion)
	}
	return nil
}
