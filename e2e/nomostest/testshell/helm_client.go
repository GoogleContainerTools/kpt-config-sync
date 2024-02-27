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
	"strings"

	"kpt.dev/configsync/e2e"
)

// HelmClient provides helm and crane clients for connecting to an Helm repository.
type HelmClient struct {
	// DockerConfig is the value to use for DOCKER_CONFIG.
	// Crane uses DOCKER_CONFIG for auth settings.
	// Helm uses DOCKER_CONFIG for default auth settings, when not specified in
	// HELM_REGISTRY_CONFIG.
	DockerConfig string

	// ConfigHome is the value to use for HELM_CONFIG_HOME.
	//
	// Defaults:
	// - Linux: ${HOME}/.config/helm
	// - Mac: ${HOME}/Library/Preferences/helm
	//   https://github.com/helm/helm/blob/main/pkg/helmpath/lazypath_darwin.go#L28
	// - Windows: ${APPDATA}/helm
	//   https://github.com/helm/helm/blob/main/pkg/helmpath/lazypath_windows.go#L22
	//
	// Affects default values for HELM_REGISTRY_CONFIG, HELM_REPOSITORY_CONFIG
	// HELM_REPOSITORY_CACHE, and HELM_CACHE_HOME.
	ConfigHome string

	// Shell used to execute helm and crane client commands.
	Shell *TestShell
}

// NewHelmClient constructs a new HelmClient that stores config in
// sub-directories under the specified `parentPath`.
func NewHelmClient(parentPath string, shell *TestShell) *HelmClient {
	cfg := &HelmClient{
		DockerConfig: filepath.Join(parentPath, "helm-docker"),
		ConfigHome:   filepath.Join(parentPath, "helm"),
		Shell: &TestShell{
			Context: shell.Context,
			Env:     append([]string{}, shell.Env...), // Copy parent shell environment
			Logger:  shell.Logger,
		},
	}
	// Add environment vars to be used for all commands with this shell.
	// Because the shell is a copy, it won't affect the parent shell.
	cfg.Shell.Env = append(cfg.Shell.Env,
		fmt.Sprintf("DOCKER_CONFIG=%s", cfg.DockerConfig),
		fmt.Sprintf("HELM_CONFIG_HOME=%s", cfg.ConfigHome))

	shell.Logger.Infof("DOCKER_CONFIG for helm will be created under %s", cfg.DockerConfig)
	shell.Logger.Infof("HELM_CONFIG_HOME will be created under %s", cfg.ConfigHome)
	shell.Logger.Infof("Connect to the %q Helm registry using:\nexport DOCKER_CONFIG=%s\nexport HELM_CONFIG_HOME=%s",
		*e2e.HelmProvider, cfg.DockerConfig, cfg.ConfigHome)

	return cfg
}

// Login uses gcloud to configure docker auth credentials used by helm and crane.
//
// Files modified:
// - `${DOCKER_CONFIG}/config.json` (crane/docker)
func (hc *HelmClient) Login(registryHost string) error {
	if _, err := hc.Shell.ExecWithDebug("gcloud", "auth", "configure-docker", "--quiet", registryHost); err != nil {
		return fmt.Errorf("gcloud auth configure-docker: %w", err)
	}
	return nil
}

// Logout removes credentials for the specified registry.
//
// Files modified:
// - `${DOCKER_CONFIG}/config.json` (crane/docker)
// - `${HELM_CONFIG_HOME}/registry/config.json` (helm)
func (hc *HelmClient) Logout(registryHost string) error {
	if out, err := hc.Helm("registry", "logout", registryHost); err != nil {
		// helm doesn't logout idempotently, so ignore not logged in errors.
		if strings.TrimSpace(string(out)) != "Error: not logged in" {
			return fmt.Errorf("helm registry logout: %w", err)
		}
	}
	if _, err := hc.Crane("auth", "logout", registryHost); err != nil {
		return fmt.Errorf("crane auth logout: %w", err)
	}
	// TODO: Figure out how to remove a credHelper config cleanly. There's no docker command for it.
	return nil
}

// Helm executes a helm command with the HelmClient's config.
func (hc *HelmClient) Helm(args ...string) ([]byte, error) {
	return hc.Shell.ExecWithDebug("helm", args...)
}

// Crane executes a crane command with the HelmClient's config.
func (hc *HelmClient) Crane(args ...string) ([]byte, error) {
	return hc.Shell.ExecWithDebug("crane", args...)
}
