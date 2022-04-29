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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"sync"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/client-go/rest"

	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/version"
)

func init() {
	flags.AddContexts(Cmd)
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", flags.DefaultClusterClientTimeout, "Timeout for connecting to each cluster")
}

// GetVersionReadCloser returns a ReadCloser with the output produced by running the "nomos version" command as a string
func GetVersionReadCloser(ctx context.Context, contexts []string) (io.ReadCloser, error) {
	r, w, _ := os.Pipe()
	writer := util.NewWriter(w)
	allCfgs, err := allKubectlConfigs()
	if err != nil {
		return nil, err
	}

	versionInternal(ctx, allCfgs, writer, contexts)
	err = w.Close()
	if err != nil {
		return nil, errors.Wrap(err, "failed to close version file writer with error: %v")
	}

	return ioutil.NopCloser(r), nil
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
		RunE: func(cmd *cobra.Command, args []string) error {
			// Don't show usage on error, as argument validation passed.
			cmd.SilenceUsage = true

			allCfgs, err := allKubectlConfigs()
			versionInternal(cmd.Context(), allCfgs, os.Stdout, flags.Contexts)

			if err != nil {
				return errors.Wrap(err, "unable to parse kubectl config")
			}
			return nil
		},
	}
)

// allKubectlConfigs gets all kubectl configs, with error handling
func allKubectlConfigs() (map[string]*rest.Config, error) {
	allCfgs, err := restconfig.AllKubectlConfigs(flags.ClientTimeout)
	if err != nil {
		// Unwrap the "no such file or directory" error for better readability
		if unWrapped := errors.Cause(err); os.IsNotExist(unWrapped) {
			err = unWrapped
		}

		// nolint:errcheck
		fmt.Printf("failed to create client configs: %v\n", err)
	}

	return allCfgs, err
}

// versionInternal allows stubbing out the config for tests.
func versionInternal(ctx context.Context, configs map[string]*rest.Config, w io.Writer, contexts []string) {
	// See go/nomos-cli-version-design for the output below.
	if contexts != nil {
		// filter by specified contexts
		configs = filterConfigs(contexts, configs)
		if len(configs) == 0 {
			fmt.Print("No clusters match the specified context.\n")
		}
	}

	vs, monoRepoClusters := versions(ctx, configs)
	// Log a notice for the detected clusters that are running in the mono-repo mode.
	util.MonoRepoNotice(w, monoRepoClusters...)
	es := entries(vs)
	tabulate(es, w)
}

// filterConfigs retains from all only the configs that have been selected
// through flag use. contexts is the list of contexts to print information for.
// If allClusters is true, contexts is ignored and information for all contexts
// is printed.
func filterConfigs(contexts []string, all map[string]*rest.Config) map[string]*rest.Config {
	cfgs := make(map[string]*rest.Config)
	for _, name := range contexts {
		if cfg, ok := all[name]; ok {
			cfgs[name] = cfg
		}
	}
	return cfgs
}

func lookupVersionAndMode(ctx context.Context, cfg *rest.Config) (string, *bool, error) {
	cmClient, err := util.NewConfigManagementClient(cfg)
	if err != nil {
		return util.ErrorMsg, nil, err
	}
	v, err := cmClient.Version(ctx)
	if err != nil {
		return util.ErrorMsg, nil, err
	}
	isMulti, err := cmClient.IsMultiRepo(ctx)
	if apierrors.IsNotFound(err) {
		return v, isMulti, nil
	}
	return v, isMulti, err
}

// vErr is either a version or an error.
type vErr struct {
	version string
	err     error
}

// versions obtains the versions of all configmanagements from the contexts
// supplied in the named configs, and a list of clusters running in the mono-repo mode.
func versions(ctx context.Context, cfgs map[string]*rest.Config) (map[string]vErr, []string) {
	var monoRepoClusters []string
	if len(cfgs) == 0 {
		return nil, nil
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
			var isMulti *bool
			ve.version, isMulti, ve.err = lookupVersionAndMode(ctx, c)
			if isMulti != nil && !*isMulti {
				monoRepoClusters = append(monoRepoClusters, n)
			}
			m.Lock()
			vs[n] = ve
			m.Unlock()
		}(n, c)
	}
	g.Wait()
	return vs, monoRepoClusters
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
		fmt.Printf("Failed to get current context name with err: %v\n", errors.Cause(err))
	}

	var es []entry
	for n, v := range vs {
		curr := ""
		if n == currentContext && err == nil {
			curr = "*"
		}
		es = append(es, entry{current: curr, name: n, component: util.ConfigManagementName, vErr: v})
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
			// nolint:errcheck
			fmt.Fprintf(os.Stderr, "error on Flush(): %v", err)
		}
	}()
	// nolint:errcheck
	fmt.Fprintf(w, format, "CURRENT", "CLUSTER_CONTEXT_NAME", "COMPONENT", "VERSION")
	for _, e := range es {
		if e.err != nil {
			// nolint:errcheck
			fmt.Fprintf(w, format, e.current, e.name, e.component, fmt.Sprintf("<error: %v>", e.err))
			continue
		}
		// nolint:errcheck
		fmt.Fprintf(w, format, e.current, e.name, e.component, e.version)
	}
}
