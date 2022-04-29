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

package restconfig

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const kubectlConfigPath = ".kube/config"

// The function to use to get default current user.  Can be changed for tests
// using SetCurrentUserForTest.
var userCurrentTestHook = defaultGetCurrentUser

func defaultGetCurrentUser() (*user.User, error) {
	return user.Current()
}

// newConfigPath returns the correct kubeconfig file path to use, depending on
// the current user settings and the runtime environment.
func newConfigPath() (string, error) {
	// First try the KUBECONFIG variable.
	envPath := os.Getenv("KUBECONFIG")
	if envPath != "" {
		return envPath, nil
	}
	// Try the current user.
	curentUser, err := userCurrentTestHook()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get current user")
	}
	path := filepath.Join(curentUser.HomeDir, kubectlConfigPath)
	return path, nil
}

// newConfigFromPath creates a rest.Config from a configuration file at the
// supplied path.
func newConfigFromPath(path string) (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// newRawConfigWithRules returns a clientcmdapi.Config from a configuration file whose path is
// provided by newConfigPath, and the clientcmd.ClientConfigLoadingRules associated with it
func newRawConfigWithRules() (*clientcmdapi.Config, *clientcmd.ClientConfigLoadingRules, error) {
	configPath, err := newConfigPath()
	if err != nil {
		return nil, nil, errors.Wrap(err, "while getting config path")
	}

	rules := &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(configPath)}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	apiCfg, err := clientCfg.RawConfig()
	if err != nil {
		return nil, nil, errors.Wrap(err, "while building client config")
	}

	return &apiCfg, rules, nil
}

// CurrentContextName returns the name of the currently active k8s context as a string
// Can be changed in tests by reassigning this pointer.
var CurrentContextName = currentContextNameFromConfig

// currentContextNameFromConfig returns the name of the user's currently active context as a string.
// This information is read from the local kubeconfig file.
func currentContextNameFromConfig() (string, error) {
	apiCfg, _, err := newRawConfigWithRules()
	if err != nil {
		return "", err
	}
	return apiCfg.CurrentContext, nil
}

// AllKubectlConfigs creates a config for every context available in the kubeconfig. The configs are
// mapped by context name. There is no way to detect unhealthy clusters specified by a context, so
// timeout can be used to prevent calls to those clusters from hanging for long periods of time.
func AllKubectlConfigs(timeout time.Duration) (map[string]*rest.Config, error) {
	apiCfg, rules, err := newRawConfigWithRules()
	if err != nil {
		return nil, err
	}

	var badConfigs []string
	configs := map[string]*rest.Config{}
	for ctxName := range apiCfg.Contexts {
		cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			rules, &clientcmd.ConfigOverrides{CurrentContext: ctxName})
		restCfg, err2 := cfg.ClientConfig()
		if err2 != nil {
			badConfigs = append(badConfigs, fmt.Sprintf("%q: %v", ctxName, err2))
			continue
		}

		if timeout > 0 {
			restCfg.Timeout = timeout
		}
		configs[ctxName] = restCfg
	}

	var cfgErrs error
	if len(badConfigs) > 0 {
		cfgErrs = fmt.Errorf("failed to build configs:\n%s", strings.Join(badConfigs, "\n"))
	}
	return configs, cfgErrs
}

// newKubectlConfig creates a config for whichever context is active in kubectl.
func newKubectlConfig() (*rest.Config, error) {
	path, err := newConfigPath()
	if err != nil {
		return nil, errors.Wrapf(err, "while getting config path")
	}
	config, err := newConfigFromPath(path)
	if err != nil {
		return nil, errors.Wrapf(err, "while loading from %v", path)
	}
	return config, nil
}

// newLocalClusterConfig creates a config for connecting to the local cluster API server.
func newLocalClusterConfig() (*rest.Config, error) {
	return rest.InClusterConfig()
}
