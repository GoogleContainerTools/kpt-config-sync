// Copyright 2024 Google LLC
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

package testshell

import (
	"fmt"
	"path/filepath"

	"kpt.dev/configsync/e2e"
)

// OCIClient provides a crane client for connecting to an OCI repository.
type OCIClient struct {
	// DockerConfig is the value to use for DOCKER_CONFIG.
	// Crane uses DOCKER_CONFIG for auth settings.
	DockerConfig string

	// Shell used to execute crane client commands.
	Shell *TestShell
}

// NewOCIClient constructs a new OCIClient that stores config in sub-directories
// under the specified `parentPath`.
func NewOCIClient(parentPath string, shell *TestShell) *OCIClient {
	cfg := &OCIClient{
		DockerConfig: filepath.Join(parentPath, "oci-docker"),
		Shell: &TestShell{
			Context: shell.Context,
			Env:     append([]string{}, shell.Env...), // Copy parent shell environment
			Logger:  shell.Logger,
		},
	}

	// Add environment vars to be used for all commands with this shell.
	// Because the shell is a copy, it won't affect the parent shell.
	cfg.Shell.Env = append(cfg.Shell.Env,
		fmt.Sprintf("DOCKER_CONFIG=%s", cfg.DockerConfig))

	shell.Logger.Infof("DOCKER_CONFIG for oci will be created under %s", cfg.DockerConfig)
	shell.Logger.Infof("Connect to the %q OCI registry using:\nexport DOCKER_CONFIG=%s",
		*e2e.HelmProvider, cfg.DockerConfig)

	return cfg
}

// Login uses gcloud to configure docker auth credentials used by crane.
//
// Files modified:
// - `${DOCKER_CONFIG}/config.json` (crane/docker)
func (oc *OCIClient) Login(registryHost string) error {
	if _, err := oc.Shell.ExecWithDebug("gcloud", "auth", "configure-docker", "--quiet", registryHost); err != nil {
		return fmt.Errorf("gcloud auth configure-docker: %w", err)
	}
	return nil
}

// Logout removes credentials for the specified registry.
//
// Files modified:
// - `${DOCKER_CONFIG}/config.json` (crane/docker)
func (oc *OCIClient) Logout(registryHost string) error {
	if _, err := oc.Crane("auth", "logout", registryHost); err != nil {
		return fmt.Errorf("crane auth logout: %w", err)
	}
	// TODO: Figure out how to remove a credHelper config cleanly. There's no docker command for it.
	return nil
}

// Crane executes a crane command with the OCIClient's config.
func (oc *OCIClient) Crane(args ...string) ([]byte, error) {
	return oc.Shell.ExecWithDebug("crane", args...)
}
