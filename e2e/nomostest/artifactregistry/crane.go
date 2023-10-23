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
)

// SetupCraneImage creates a new CraneImage for use during an e2e test, along
// with its registry, repository, and login.
// Returns a reference to the Image object and any errors.
func SetupCraneImage(nt *nomostest.NT, name, version string) (*CraneImage, error) {
	image, err := SetupImage(nt, name, version)
	if err != nil {
		return nil, err
	}
	craneImage := &CraneImage{
		Image: image,
	}
	if err := craneImage.RegistryLogin(); err != nil {
		return nil, err
	}
	return craneImage, nil
}

// CraneImage represents a remote OCI-based crane-build image
type CraneImage struct {
	*Image
}

// RegistryLogin will log into the registry with crane using a gcloud auth token.
func (r *CraneImage) RegistryLogin() error {
	var err error
	authCmd := r.Shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := r.Shell.Command("crane", "auth", "login",
		"-u", "oauth2accesstoken", "--password-stdin",
		r.RegistryHost())
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

// Push will package and push the image located at r.BuildPath to the remote registry.
func (r *CraneImage) Push() error {
	r.Logger.Infof("Packaging image: %s:%s", r.Name, r.Version)
	// Use Tgz instead of just Tar so it's smaller.
	parentPath := filepath.Dir(r.BuildPath)
	localSourceTgzPath := filepath.Join(parentPath, fmt.Sprintf("%s-%s.tgz", r.Name, r.Version))
	if _, err := r.Shell.ExecWithDebug("tar", "-cvzf", localSourceTgzPath, r.BuildPath); err != nil {
		return fmt.Errorf("packaging image: %w", err)
	}
	r.Logger.Infof("Pushing image: %s:%s", r.Name, r.Version)
	_, err := r.Shell.ExecWithDebug("crane", "append",
		"-f", localSourceTgzPath,
		"-t", fmt.Sprintf("%s:%s", r.Address(), r.Version))
	if err != nil {
		return fmt.Errorf("pushing image: %w", err)
	}
	// Remove local tgz to allow re-push with the same name & version.
	// No need to defer. The whole TmpDir will be deleted in test cleanup.
	if err := os.Remove(localSourceTgzPath); err != nil {
		return fmt.Errorf("deleting local image package: %w", err)
	}
	return nil
}
