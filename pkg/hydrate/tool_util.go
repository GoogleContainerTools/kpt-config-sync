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

package hydrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	clientdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kmetrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"kpt.dev/configsync/pkg/validate"
	"kpt.dev/configsync/pkg/vet"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	// HelmVersion is the recommended version of Helm for hydration.
	HelmVersion = "v3.15.3-gke.1"
	// KustomizeVersion is the recommended version of Kustomize for hydration.
	KustomizeVersion = "v5.4.2-gke.0"
	// Helm is the binary name of the installed Helm.
	Helm = "helm"
	// Kustomize is the binary name of the installed Kustomize.
	Kustomize = "kustomize"

	maxRetries = 5
)

var (
	semverRegex             = regexp.MustCompile(semver.SemVerRegex)
	validKustomizationFiles = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}
)

// needsKustomize checks if there is a Kustomization config file under the directory.
func needsKustomize(dir string) (bool, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("unable to traverse the directory: %s: %w", dir, err)
	}
	for _, f := range files {
		if HasKustomization(filepath.Base(f.Name())) {
			return true, nil
		}
	}
	return false, nil
}

// HasKustomization checks if the file is a Kustomize configuration file.
func HasKustomization(filename string) bool {
	for _, kustomization := range validKustomizationFiles {
		if filename == kustomization {
			return true
		}
	}
	return false
}

// hasKustomizeSubdir checks if there exists a kustomization config file in any
// of the subdirectory under dir.
func hasKustomizeSubdir(dir string) (bool, error) {
	found := false
	err := filepath.Walk(dir,
		func(_ string, fi os.FileInfo, err error) error {
			if found {
				return nil
			}
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			if HasKustomization(fi.Name()) {
				found = true
			}
			return nil
		})
	return found, err
}

// mustDeleteOutput deletes the hydrated output directory with retries.
// It will exit if all attempts failed.
func mustDeleteOutput(err error, output string) {
	retries := 0
	for retries < maxRetries {
		err := os.RemoveAll(output)
		if err == nil {
			return
		}
		klog.Errorf("Unable to delete directory %s: %v", output, err)
		retries++
	}
	if err != nil {
		klog.Error(err)
	}
	klog.Fatalf("Attempted to delete the output directory %s for %d times, but all failed. Exiting now...", output, retries)
}

// kustomizeBuild runs the 'kustomize build' command to render the configs.
func kustomizeBuild(input, output string, sendMetrics bool) HydrationError {
	// The `--enable-alpha-plugins` and `--enable-exec` flags are to support rendering
	// Helm charts using the Helm inflation function.
	// The `--enable-helm` flag is to enable use of the Helm chart inflator generator.
	// We decided to enable all the flags so that both the Helm plugin and Helm
	// inflation function are supported. This provides us with a fallback plan
	// if the new Helm inflation function is having issues.
	// It has no side-effect if no Helm chart in the DRY configs.
	// The `--load-restrictor` flag is to allow files to be loaded from outside the root directory
	args := []string{"--enable-alpha-plugins", "--enable-exec", "--enable-helm", "--load-restrictor=LoadRestrictionsNone", "--output", output}

	if _, err := os.Stat(output); err == nil {
		mustDeleteOutput(err, output)
	}

	fileMode := os.FileMode(0755)
	if err := os.MkdirAll(output, fileMode); err != nil {
		return NewInternalError(fmt.Errorf("unable to make directory: %s: %w", output, err))
	}

	// run kustomize build with the wrapper library
	out, err := kmetrics.RunKustomizeBuild(context.Background(), sendMetrics, input, args...)
	if err != nil {
		kustomizeErr := fmt.Errorf("failed to run kustomize build in %s, stdout: %s: %w", input, out, err)
		mustDeleteOutput(kustomizeErr, output)
		return NewActionableError(kustomizeErr)
	}

	return nil
}

// validateTool checks if the hydration tool is installed and if the installed
// version meets the required version.
func validateTool(tool, version, requiredVersion string) error {
	matches := semverRegex.FindStringSubmatch(version)
	if len(matches) == 0 {
		return fmt.Errorf("unable to detect %s version from %q. The recommended version is %s",
			tool, version, requiredVersion)
	}
	detectedVersion, err := semver.NewVersion(matches[0])
	if err != nil {
		return err
	}
	requiredSemVersion, err := semver.NewVersion(requiredVersion)
	if err != nil {
		return err
	}
	if detectedVersion.LessThan(requiredSemVersion) {
		return fmt.Errorf("The current %s version is %q. The recommended version is %s. Please upgrade to the %s+ for compatibility.",
			tool, detectedVersion, requiredVersion, requiredVersion)
	}
	return nil
}

func getVersion(tool string) (string, error) {
	args := []string{"version", "--short"}
	out, err := exec.Command(tool, args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))
	// remove the curly braces for the kustomize output
	version = strings.TrimPrefix(version, "{")
	version = strings.TrimSuffix(version, "}")
	version = strings.TrimSpace(version)
	// remove the leading 'kustomize/' prefix for the kustomize output
	version = strings.TrimPrefix(version, "kustomize/")
	return version, nil
}

func validateKustomize() error {
	version, err := getVersion(Kustomize)
	if err != nil {
		return fmt.Errorf("Kustomization file is detected, but Kustomize is not installed: %v. Please install Kustomize and re-run the command.", err)
	}
	if err := validateTool(Kustomize, version, KustomizeVersion); err != nil {
		fmt.Printf("WARNING: %v\n", err)
	}
	return nil
}

func validateHelm() error {
	version, err := getVersion(Helm)
	if err != nil {
		// return nil because Helm binary is optional
		// 'kustomize build' will fail if Helm is needed but not installed
		return nil
	}
	if err := validateTool(Helm, version, HelmVersion); err != nil {
		fmt.Printf("WARNING: %v\n", err)
	}
	return nil
}

// ValidateAndRunKustomize validates if the Kustomize and Helm binaries are supported.
// If supported, it copies the source configs to a temp directory, run 'kustomize build',
// save the output to another temp directory, and return the output path for further
// parsing and validation.
func ValidateAndRunKustomize(sourcePath string) (cmpath.Absolute, error) {
	var output cmpath.Absolute
	if err := validateKustomize(); err != nil {
		return output, err
	}
	if err := validateHelm(); err != nil {
		return output, err
	}

	// Save the 'kustomize build' output to a temp directory for further
	// parsing or validation.
	tmpHydratedDir, err := os.MkdirTemp(os.TempDir(), "hydrated-")
	if err != nil {
		return output, err
	}

	if err := kustomizeBuild(sourcePath, tmpHydratedDir, false); err != nil {
		return output, fmt.Errorf("unable to render the source configs in %s: %w", sourcePath, err)
	}

	fmt.Println("NOTICE: The command will save the remote Helm charts to a local directory defined in the `helmGlobals.chartHome` field if the Kustomization file references remote Helm charts. " +
		"The default value is `charts`, which is relative to the Kustomization root. Please delete or ignore the directory in your Git repository.")
	return cmpath.AbsoluteOS(tmpHydratedDir)
}

// ValidateHydrateFlags validates the hydrate and vet flags.
// It returns the absolute path of the source directory, if hydration is needed, and errors.
func ValidateHydrateFlags(sourceFormat configsync.SourceFormat) (cmpath.Absolute, bool, error) {
	abs, err := filepath.Abs(flags.Path)
	if err != nil {
		return "", false, err
	}
	rootDir, err := cmpath.AbsoluteOS(abs)
	if err != nil {
		return "", false, err
	}
	rootDir, err = rootDir.EvalSymlinks()
	if err != nil {
		return "", false, err
	}

	switch flags.OutputFormat {
	case flags.OutputYAML, flags.OutputJSON: // do nothing
	default:
		return "", false, fmt.Errorf("format argument must be %q or %q", flags.OutputYAML, flags.OutputJSON)
	}

	needsKustomize, err := needsKustomize(abs)
	if err != nil {
		return "", false, fmt.Errorf("unable to check if Kustomize is needed for the source directory: %s: %w", abs, err)
	}

	if needsKustomize && sourceFormat == configsync.SourceFormatHierarchy {
		return "", false, fmt.Errorf("%s must be %s when Kustomization is needed", reconcilermanager.SourceFormat, configsync.SourceFormatUnstructured)
	}

	return rootDir, needsKustomize, nil
}

// ValidateOptions returns the validate options for nomos hydrate and vet commands.
func ValidateOptions(ctx context.Context, rootDir cmpath.Absolute, apiServerTimeout time.Duration) (validate.Options, error) {
	options := validate.Options{}

	var serverResourcer discovery.ServerResourcer = discovery.NoOpServerResourcer{}

	if !flags.SkipAPIServer {
		cfg, err := restconfig.NewRestConfig(apiServerTimeout)
		if err != nil {
			return options, apiServerCheckError(err, "failed to create rest config")
		}
		c, err := newClientClient(cfg)
		if err != nil {
			return options, err
		}
		options.PreviousCRDs, err = getSyncedCRDs(ctx, c)
		if err != nil {
			return options, err
		}
		dc, err := newDiscoveryClient(cfg)
		if err != nil {
			return options, err
		}
		serverResourcer = dc
		options.Converter, err = declared.NewValueConverter(dc)
		if err != nil {
			return options, err
		}
	}

	options.PolicyDir = cmpath.RelativeOS(rootDir.OSPath())
	options.BuildScoper = discovery.ScoperBuilder(serverResourcer,
		vet.AddCachedAPIResources(rootDir.Join(vet.APIResourcesPath)))
	options.AllowUnknownKinds = flags.SkipAPIServer
	return options, nil
}

func newClientClient(cfg *rest.Config) (client.Client, error) {
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, apiServerCheckError(err, "failed to create HTTPClient")
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		return nil, apiServerCheckError(err, "failed to create mapper")
	}
	c, err := client.New(cfg, client.Options{
		Scheme: core.Scheme,
		Mapper: mapper,
	})
	if err != nil {
		return nil, apiServerCheckError(err, "failed to create client")
	}
	return c, nil
}

func newDiscoveryClient(cfg *rest.Config) (clientdiscovery.CachedDiscoveryInterface, error) {
	cf, err := restconfig.NewConfigFlags(cfg)
	if err != nil {
		return nil, apiServerCheckError(err, "failed to create config flags from rest config")
	}
	dc, err := cf.ToDiscoveryClient()
	if err != nil {
		return nil, apiServerCheckError(err, "failed to create discovery client")
	}
	return dc, nil
}

func apiServerCheckError(err error, message string) status.Error {
	return status.APIServerError(err, message+". Did you mean to run with --no-api-server-check?")
}

// getSyncedCRDs returns the CRDs synced to the cluster in the current context.
//
// Times out after 15 seconds.
func getSyncedCRDs(ctx context.Context, c client.Client) ([]*v1beta1.CustomResourceDefinition, status.MultiError) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	configs := &namespaceconfig.AllConfigs{}
	decorateErr := namespaceconfig.DecorateWithClusterConfigs(ctx, c, configs)
	if decorateErr != nil {
		return nil, decorateErr
	}

	decoder := decode.NewGenericResourceDecoder(core.Scheme)
	syncedCRDs, crdErr := clusterconfig.GetCRDs(decoder, configs.ClusterConfig)
	if crdErr != nil {
		// We were unable to parse the CRDs from the current ClusterConfig, so bail out.
		// TODO: Make error message more user-friendly when this happens.
		return nil, crdErr
	}
	return syncedCRDs, nil
}
