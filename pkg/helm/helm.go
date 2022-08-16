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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/util"
)

// Hydrator runs the helm hydration process.
type Hydrator struct {
	Chart       string
	Repo        string
	Version     string
	ReleaseName string
	Namespace   string
	Values      string
	ValuesFiles string
	IncludeCRDs string
	HydrateRoot string
	Dest        string
	Auth        configsync.AuthType
	UserName    string
	Password    string
}

func (h *Hydrator) templateArgs(destDir string) []string {
	args := []string{"template"}
	if h.ReleaseName != "" {
		args = append(args, h.ReleaseName)
	}
	if h.isOCI() {
		args = append(args, h.Repo+"/"+h.Chart)
	} else {
		args = append(args, h.Chart)
		args = append(args, "--repo", h.Repo)
	}
	if h.Namespace != "" {
		args = append(args, "--namespace", h.Namespace)
	}
	if h.Version != "" {
		args = append(args, "--version", h.Version)
	}
	if len(h.ValuesFiles) > 0 {
		valuesFiles := strings.Split(h.ValuesFiles, ",")
		for _, fileURL := range valuesFiles {
			args = append(args, "-f", fileURL)
		}
	}
	if len(h.Values) > 0 {
		args = append(args, "--set", h.Values)
	}
	includeCRDs, _ := strconv.ParseBool(h.IncludeCRDs)
	if includeCRDs {
		args = append(args, "--include-crds")
	}
	args = append(args, "--output-dir", destDir)
	return args
}

func (h *Hydrator) registryLoginArgs(ctx context.Context) ([]string, error) {
	args := []string{"registry", "login"}
	switch h.Auth {
	case configsync.AuthToken:
		args = append(args, "--username", h.UserName)
		args = append(args, "--password", h.Password)
	case configsync.AuthGCPServiceAccount, configsync.AuthGCENode:
		token, err := fetchNewToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch new token: %w", err)
		}
		args = append(args, "--username", "oauth2accesstoken")
		args = append(args, "--password", token.AccessToken)
	}
	res := strings.Split(strings.TrimPrefix(h.Repo, "oci://"), "/")
	args = append(args, "https://"+res[0])
	return args, nil
}

func fetchNewToken(ctx context.Context) (*oauth2.Token, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}
	t, err := creds.TokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get token from credentials: %w", err)
	}
	return t, nil
}

// HelmTemplate runs helm template with args
func (h *Hydrator) HelmTemplate(ctx context.Context) error {
	//TODO: add logic to handle "latest" version
	destDir := filepath.Join(h.HydrateRoot, h.Chart+":"+h.Version)
	linkPath := filepath.Join(h.HydrateRoot, h.Dest)
	oldDir, err := filepath.EvalSymlinks(linkPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to evaluate the symbolic path %q to the Helm chart: %w", linkPath, err)
	}
	if oldDir == destDir {
		klog.Infof("no update required with the same helm chart version %q", h.Version)
		return nil
	}
	if h.Auth != configsync.AuthNone && h.isOCI() {
		args, err := h.registryLoginArgs(ctx)
		if err != nil {
			return err
		}
		out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to authenticate to helm registry: %w, stdout: %s", err, string(out))
		}
	}
	args := h.templateArgs(destDir)
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to render the helm chart: %w, stdout: %s", err, string(out))
	}
	klog.Infof("successfully rendered the helm chart : %s", string(out))
	return util.UpdateSymlink(h.HydrateRoot, linkPath, destDir, oldDir)
}

func (h *Hydrator) isOCI() bool {
	return strings.HasPrefix(h.Repo, "oci://")
}
