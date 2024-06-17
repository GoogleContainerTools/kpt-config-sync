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
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
)

const kubectlConfigPath = ".kube/config"

// The function to use to get default current user.  Can be changed for tests
// using SetCurrentUserForTest.
var userCurrentTestHook = defaultGetCurrentUser

func defaultGetCurrentUser() (*user.User, error) {
	return user.Current()
}

// KubeConfigPath returns the path to the kubeconfig:
// 1. ${KUBECONFIG}, if non-empty
// 2. ${userCurrentTestHook.HomeDir}/.kube/config, if userCurrentTestHook is set
// 3. ${HOME}/.kube/config
func KubeConfigPath() (string, error) {
	envPath := os.Getenv("KUBECONFIG")
	if envPath != "" {
		return envPath, nil
	}
	currentUser, err := userCurrentTestHook()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	path := filepath.Join(currentUser.HomeDir, kubectlConfigPath)
	return path, nil
}

// newRawConfigWithRules returns a clientcmdapi.Config from a configuration file whose path is
// provided by newConfigPath, and the clientcmd.ClientConfigLoadingRules associated with it
func newRawConfigWithRules() (*clientcmdapi.Config, *clientcmd.ClientConfigLoadingRules, error) {
	configPath, err := KubeConfigPath()
	if err != nil {
		return nil, nil, fmt.Errorf("while getting config path: %w", err)
	}

	rules := &clientcmd.ClientConfigLoadingRules{Precedence: filepath.SplitList(configPath)}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	apiCfg, err := clientCfg.RawConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("while building client config: %w", err)
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
	if len(apiCfg.Contexts) == 0 {
		return map[string]*rest.Config{}, nil
	}

	if klog.V(4).Enabled() {
		// Sort contexts for consistent ordering in the log
		var contexts []string
		for ctxName := range apiCfg.Contexts {
			contexts = append(contexts, ctxName)
		}
		sort.Strings(contexts)
		klog.V(4).Infof("Found config contexts: %s", strings.Join(contexts, ", "))
		klog.V(4).Infof("Current config context: %s", apiCfg.CurrentContext)
	}

	var badConfigs []string
	configs := make(map[string]*rest.Config, len(apiCfg.Contexts))
	for ctxName := range apiCfg.Contexts {
		cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			rules, &clientcmd.ConfigOverrides{CurrentContext: ctxName})
		restCfg, err2 := cfg.ClientConfig()
		if err2 != nil {
			badConfigs = append(badConfigs, fmt.Sprintf("%q: %v", ctxName, err2))
			continue
		}

		UpdateQPS(restCfg)

		if timeout > 0 {
			restCfg.Timeout = timeout
		}
		configs[ctxName] = restCfg
	}

	if len(badConfigs) > 0 {
		return configs, fmt.Errorf("failed to build configs:\n%s", strings.Join(badConfigs, "\n"))
	}
	return configs, nil
}
