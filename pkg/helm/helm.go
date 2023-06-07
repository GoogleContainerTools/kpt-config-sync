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

	setnamespace "github.com/GoogleContainerTools/kpt-functions-catalog/functions/go/set-namespace/transformer"
	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

const (
	// valuesFile is the name of the file created to override defualt chart values.
	valuesFile = "chart-values.yaml"
)

var (
	// helmCacheHome is the local filepath where helm writes local cache data
	helmCacheHome = os.Getenv("HOME") + "/.cache/helm"
)

// Hydrator runs the helm hydration process.
type Hydrator struct {
	Chart           string
	Repo            string
	Version         string
	ReleaseName     string
	Namespace       string
	DeployNamespace string
	Values          string
	IncludeCRDs     string
	HydrateRoot     string
	Dest            string
	Auth            configsync.AuthType
	UserName        string
	Password        string
}

func (h *Hydrator) templateArgs(ctx context.Context, destDir string) ([]string, error) {
	args := []string{"template"}
	var err error

	if h.ReleaseName != "" {
		args = append(args, h.ReleaseName)
	}
	if h.isOCI() {
		args = append(args, h.Repo+"/"+h.Chart)
	} else {
		args = append(args, h.Chart)
		args = append(args, "--repo", h.Repo)
		args, err = h.appendAuthArgs(ctx, args)
		if err != nil {
			return nil, err
		}
	}
	if h.Namespace != "" {
		args = append(args, "--namespace", h.Namespace)
	} else {
		args = append(args, "--namespace", configsync.DefaultHelmReleaseNamespace)
	}
	if h.Version != "" {
		args = append(args, "--version", h.Version)
	}
	if len(h.Values) > 0 {
		args, err = h.appendValuesArgs(args)
		if err != nil {
			return nil, err
		}
	}
	includeCRDs, _ := strconv.ParseBool(h.IncludeCRDs)
	if includeCRDs {
		args = append(args, "--include-crds")
	}
	args = append(args, "--output-dir", destDir)
	return args, nil
}

func (h *Hydrator) appendValuesArgs(args []string) ([]string, error) {
	valuesPath := filepath.Join(os.TempDir(), valuesFile)
	if err := os.WriteFile(valuesPath, []byte(h.Values), 0644); err != nil {
		return nil, fmt.Errorf("failed to create values file: %w", err)
	}
	args = append(args, "--values", valuesPath)
	return args, nil
}

func (h *Hydrator) registryLoginArgs(ctx context.Context) ([]string, error) {
	args := []string{"registry", "login"}
	args, err := h.appendAuthArgs(ctx, args)
	if err != nil {
		return nil, err
	}
	res := strings.Split(strings.TrimPrefix(h.Repo, "oci://"), "/")
	args = append(args, "https://"+res[0])
	return args, nil
}

func (h *Hydrator) showChartArgs(ctx context.Context) ([]string, error) {
	if h.isOCI() {
		return []string{"show", "chart", h.Repo + "/" + h.Chart, "--version", h.Version}, nil
	}
	return h.appendAuthArgs(ctx, []string{"show", "chart", h.Chart, "--repo", h.Repo, "--version", h.Version})
}

// figure out which version we are going to pull as it can be provided to us as a range (e.g. 1.0.0 - 1.6.5)
func (h *Hydrator) getChartVersion(ctx context.Context) error {
	// Use `helm show chart` to get chart info and parse the output to get the version number.
	// This is not super convenient but seems to be the only option that will work with OCI.
	// See available subcommands for OCI registries at https://helm.sh/docs/topics/registries/.
	args, err := h.showChartArgs(ctx)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run `helm show chart`: %w, stdout: %s", err, string(out))
	}
	var parsedOut map[string]interface{}
	if err := yaml.Unmarshal(out, &parsedOut); err != nil {
		return fmt.Errorf("failed to parse output of `helm show chart`: %w, stdout: %s", err, string(out))
	}

	version, ok := parsedOut["version"].(string)
	if ok {
		h.Version = version
	} else {
		return fmt.Errorf("failed to get version from output of `helm show chart`, stdout: %s", string(out))
	}

	// we need to clear the local helm cache after running `helm show chart`,
	// otherwise we can get an OOM error on autopilot clusters later during
	// the rendering step
	if err := os.RemoveAll(helmCacheHome); err != nil {
		// we don't necessarily need to exit on error here, as it is possible that the later rendering
		// step could still succeed, so we just log the error and continue
		klog.Infoln("failed to clear helm cache: %w", err)
	}

	return nil
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

func (h *Hydrator) setDeployNamespace(destDir string) error {
	if h.DeployNamespace == "" {
		// do nothing
		return nil
	}

	pkgReadWriter := kio.LocalPackageReadWriter{
		PackagePath: destDir,
		FileSystem:  filesys.FileSystemOrOnDisk{FileSystem: filesys.MakeFsOnDisk()},
	}

	// read the directory using kyaml and convert to kpt fn sdk KubeObjects
	var rl fn.ResourceList
	nodes, err := pkgReadWriter.Read()
	for _, node := range nodes {
		kubeObject, _ := fn.ParseKubeObject([]byte(node.MustString()))
		rl.Items = append(rl.Items, kubeObject)
	}
	if err != nil {
		return err
	}

	// run the kpt set-namespace fn as a library
	rl.FunctionConfig, err = fn.ParseKubeObject([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: kptfile.kpt.dev
data:
  name: ` + h.DeployNamespace))
	if err != nil {
		return err
	}
	if _, err := setnamespace.Run(&rl); err != nil {
		return err
	}

	// convert transformed objects back to kyaml RNodes before writing the output
	var newNodes []*yaml.RNode
	for _, obj := range rl.Items {
		newNodes = append(newNodes, yaml.MustParse(obj.String()))
	}

	return pkgReadWriter.Write(newNodes)
}

// HelmTemplate runs helm template with args
func (h *Hydrator) HelmTemplate(ctx context.Context) error {
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

	if err := h.getChartVersion(ctx); err != nil {
		return err
	}

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

	args, err := h.templateArgs(ctx, destDir)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to render the helm chart: %w, stdout: %s", err, string(out))
	}

	if err := h.setDeployNamespace(destDir); err != nil {
		return fmt.Errorf("failed to set the deploy namespace: %w", err)
	}

	klog.Infof("successfully rendered the helm chart : %s", string(out))
	return util.UpdateSymlink(h.HydrateRoot, linkPath, destDir, oldDir)
}

func (h *Hydrator) isOCI() bool {
	return strings.HasPrefix(h.Repo, "oci://")
}

func (h *Hydrator) appendAuthArgs(ctx context.Context, args []string) ([]string, error) {
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
	return args, nil
}
