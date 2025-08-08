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

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

// VERSION is the semver version of this application.
var VERSION = "UNKNOWN"

// GetVersionReadCloser returns a ReadCloser with the output produced by running the "nomos version" command as a string
func GetVersionReadCloser(ctx context.Context) (io.ReadCloser, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	writer := util.NewWriter(w)
	allCfgs, err := AllKubectlConfigs()
	if err != nil {
		return nil, err
	}

	Print(ctx, allCfgs, writer)
	err = w.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close version file writer with error: %w", err)
	}

	return io.NopCloser(r), nil
}

// AllKubectlConfigs gets all kubectl configs, with error handling
func AllKubectlConfigs() (map[string]*rest.Config, error) {
	allCfgs, err := restconfig.AllKubectlConfigs(flags.ClientTimeout, flags.Contexts)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr
		}

		return nil, fmt.Errorf("failed to create client configs: %v\n", err)
	}

	return allCfgs, nil
}

// Print writes version information for the nomos CLI and each cluster to w.
func Print(ctx context.Context, configs map[string]*rest.Config, w io.Writer) {
	// See go/nomos-cli-version-design for the output below.

	if len(configs) == 0 {
		fmt.Print("No clusters match the specified context.\n")
	}

	vs, monoRepoClusters := versions(ctx, configs)
	// Log a notice for the detected clusters that are running in the mono-repo mode.
	util.MonoRepoNotice(w, monoRepoClusters...)
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
// and will return ACM version, isMultiRepo state, installation type, and error if needed
// OSS installation will check version from image used, while operator will check from ConfigManagement
func lookupVersionAndMode(ctx context.Context, cfg *rest.Config) (string, *bool, string, error) {
	cmClient, err := util.NewConfigManagementClient(cfg)
	if err != nil {
		return util.ErrorMsg, nil, "", err
	}
	if cfg != nil {
		cl, err := ctrl.New(cfg, ctrl.Options{})
		if err != nil {
			return util.ErrorMsg, nil, "", err
		}
		ck, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return util.ErrorMsg, nil, "", err
		}
		isOss, err := util.IsOssInstallation(ctx, cmClient, cl, ck)
		if err != nil {
			return util.ErrorMsg, nil, "", err
		}
		if isOss {
			reconcilerDeployment, err := ck.AppsV1().Deployments(configmanagement.ControllerNamespace).Get(ctx, util.ReconcilerManagerName, metav1.GetOptions{})
			if err != nil {
				return util.ErrorMsg, nil, "", err
			}
			imageVersion, err := getImageVersion(reconcilerDeployment)
			if err != nil {
				return util.ErrorMsg, nil, "", err
			}
			return imageVersion, &isOss, util.ConfigSyncName, nil
		}
	}

	v, err := cmClient.Version(ctx)
	if err != nil {
		return util.ErrorMsg, nil, "", err
	}
	isMulti, err := cmClient.IsMultiRepo(ctx)
	if apierrors.IsNotFound(err) {
		return v, isMulti, util.ConfigManagementName, nil
	}
	return v, isMulti, util.ConfigManagementName, err
}

// vErr is either a version or an error.
type vErr struct {
	version   string
	component string
	err       error
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
			ve.version, isMulti, ve.component, ve.err = lookupVersionAndMode(ctx, c)
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
		vErr:      vErr{version: VERSION, err: nil}})
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
