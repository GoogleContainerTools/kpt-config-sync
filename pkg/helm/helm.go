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
	"strings"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/util"
)

// Hydrator runs the helm hydration process.
type Hydrator struct {
	Chart       string
	Repo        string
	Version     string
	ReleaseName string
	Namespace   string
	HydrateRoot string
	Dest        string
}

func (h *Hydrator) templateArgs(destDir string) []string {
	args := []string{"template"}
	if h.ReleaseName != "" {
		args = append(args, h.ReleaseName)
	}
	if h.isOCI() {
		args = append(args, filepath.Join(h.Repo, h.Chart))
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
	//TODO: add logic for values/crd update.
	args = append(args, "--output-dir", destDir)
	return args
}

// HelmTemplate runs helm template with args
func (h *Hydrator) HelmTemplate(ctx context.Context) error {
	//TODO: add logic to handle "latest" version
	destDir := filepath.Join(h.HydrateRoot, h.Chart+h.Version)
	linkPath := filepath.Join(h.HydrateRoot, h.Dest)
	oldDir, err := filepath.EvalSymlinks(linkPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to evaluate the symbolic path %q to the Helm chart: %w", linkPath, err)
	}
	if oldDir == destDir {
		klog.Infof("no update required with the same helm chart version %q", h.Version)
		return nil
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
