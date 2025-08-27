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

package version

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/version"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	flags.AddContexts(Cmd)
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", restconfig.DefaultTimeout, "Timeout for connecting to each cluster")
}

// GetVersionReadCloser returns a ReadCloser with the output produced by running the "nomos version" command as a string
func GetVersionReadCloser(ctx context.Context) (io.ReadCloser, error) {
	r, w, _ := os.Pipe()
	writer := util.NewWriter(w)
	allCfgs, err := allKubectlConfigs()
	if err != nil {
		return nil, err
	}

	versionInternal(ctx, allCfgs, writer)
	err = w.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close version file writer with error: %w", err)
	}

	return io.NopCloser(r), nil
}

var (
	// clientVersion is a function that obtains the local client version.
	clientVersion = func() string {
		return version.VERSION
	}

	// Cmd is the Cobra object representing the nomos version command.
	Cmd = &cobra.Command{
		Use:   "version",
		Short: "Prints the version of ACM for each cluster as well this CLI",
		Long: `Prints the version of Configuration Management installed on each cluster and the version
of the "nomos" client binary for debugging purposes.`,
		Example: `  nomos version`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Don't show usage on error, as argument validation passed.
			cmd.SilenceUsage = true

			allCfgs, err := allKubectlConfigs()
			versionInternal(cmd.Context(), allCfgs, os.Stdout)

			if err != nil {
				return fmt.Errorf("unable to parse kubectl config: %w", err)
			}
			return nil
		},
	}
)

// allKubectlConfigs gets all kubectl configs, with error handling
func allKubectlConfigs() (map[string]*rest.Config, error) {
	allCfgs, err := restconfig.AllKubectlConfigs(flags.ClientTimeout, flags.Contexts)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr
		}

		fmt.Printf("failed to create client configs: %v\n", err)
	}

	return allCfgs, err
}

// versionInternal allows stubbing out the config for tests.
func versionInternal(ctx context.Context, configs map[string]*rest.Config, w io.Writer) {
	// See go/nomos-cli-version-design for the output below.

	if len(configs) == 0 {
		fmt.Print("No clusters match the specified context.\n")
	}

	vs := versions(ctx, configs)

	es := entries(vs)
	tabulate(es, w)
}

func getImageVersion(deployment *v1.Deployment) (string, error) {
	var container corev1.Container
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if c.Name == util.ReconcilerManagerName {
			container = c
			break
		}
	}

	reconcilerManagerImage := strings.Split(container.Image, ":")
	if len(reconcilerManagerImage) <= 1 {
		return "", fmt.Errorf("Failed to get valid image version from: %s", reconcilerManagerImage)
	}
	return reconcilerManagerImage[1], nil
}

// lookupVersionAndMode will check for installation type (oss or operator)
// and will return ACM version, installation type, and error if needed
// OSS installation will check version from image used, while operator will check from ConfigManagement
func lookupVersionAndMode(ctx context.Context, cfg *rest.Config) (string, string, error) {
	cmClient, err := util.NewConfigManagementClient(cfg)
	if err != nil {
		return util.ErrorMsg, "", err
	}
	if cfg != nil {
		cl, err := ctrl.New(cfg, ctrl.Options{})
		if err != nil {
			return util.ErrorMsg, "", err
		}
		ck, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return util.ErrorMsg, "", err
		}
		isOss, err := util.IsOssInstallation(ctx, cmClient, cl, ck)
		if err != nil {
			return util.ErrorMsg, "", err
		}
		if isOss {
			reconcilerDeployment, err := ck.AppsV1().Deployments(configmanagement.ControllerNamespace).Get(ctx, util.ReconcilerManagerName, metav1.GetOptions{})
			if err != nil {
				return util.ErrorMsg, "", err
			}
			imageVersion, err := getImageVersion(reconcilerDeployment)
			if err != nil {
				return util.ErrorMsg, "", err
			}
			return imageVersion, util.ConfigSyncName, nil
		}
	}

	v, err := cmClient.Version(ctx)
	if err != nil {
		return util.ErrorMsg, "", err
	}
	return v, util.ConfigManagementName, err
}

// vErr is either a version or an error.
type vErr struct {
	version   string
	component string
	err       error
}

// versions obtains the versions of all configmanagements from the contexts supplied in
// the named configs
func versions(ctx context.Context, cfgs map[string]*rest.Config) map[string]vErr {
	if len(cfgs) == 0 {
		return nil
	}
	vs := make(map[string]vErr, len(cfgs))
	var (
		m sync.Mutex // GUARDS vs
		g sync.WaitGroup
	)
	for n, c := range cfgs {
		g.Add(1)
		go func(n string, c *rest.Config) {
			defer g.Done()
			var ve vErr
			ve.version, ve.component, ve.err = lookupVersionAndMode(ctx, c)
			m.Lock()
			vs[n] = ve
			m.Unlock()
		}(n, c)
	}
	g.Wait()
	return vs
}

// entry is one entry of the output
type entry struct {
	// current denotes the current context. the value is a '*' if this is the current context,
	// or any empty string otherwise
	current string
	// name is the context's name.
	name string
	// component is the nomos component name.
	component string
	// vErr is either a version or an error.
	vErr
}

// entries produces a stable list of version reports based on the unordered
// versions provided.
func entries(vs map[string]vErr) []entry {
	currentContext, err := restconfig.CurrentContextName()
	if err != nil {
		fmt.Printf("Failed to get current context name with err: %v\n", err)
	}

	var es []entry
	for n, v := range vs {
		curr := ""
		if n == currentContext && err == nil {
			curr = "*"
		}
		es = append(es, entry{current: curr, name: n, component: v.component, vErr: v})
	}
	// Also fill in the client version here.
	es = append(es, entry{
		component: "<nomos CLI>",
		vErr:      vErr{version: clientVersion(), err: nil}})
	sort.SliceStable(es, func(i, j int) bool {
		return es[i].name < es[j].name
	})
	return es
}

// tabulate prints out the findings in the provided entries in a nice tabular
// form.  It's the sixties, go for it!
func tabulate(es []entry, out io.Writer) {
	format := "%s\t%s\t%s\t%s\n"
	w := util.NewWriter(out)
	defer func() {
		if err := w.Flush(); err != nil {
			util.MustFprintf(os.Stderr, "error on Flush(): %v", err)
		}
	}()
	util.MustFprintf(w, format, "CURRENT", "CLUSTER_CONTEXT_NAME", "COMPONENT", "VERSION")
	for _, e := range es {
		if e.err != nil {
			util.MustFprintf(w, format, e.current, e.name, e.component, fmt.Sprintf("<error: %v>", e.err))
			continue
		}
		util.MustFprintf(w, format, e.current, e.name, e.component, e.version)
	}
}
