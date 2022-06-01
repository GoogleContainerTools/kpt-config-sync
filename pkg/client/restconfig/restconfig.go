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
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // kubectl auth provider plugins
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// DefaultTimeout is the default REST config timeout.
const DefaultTimeout = 5 * time.Second

// A source for creating a rest config
type configSource struct {
	name   string                       // The name for the config
	create func() (*rest.Config, error) // The function for creating the config
}

// List of config sources that will be tried in order for creating a rest.Config
var configSources = []configSource{
	{
		name:   "podServiceAccount",
		create: newLocalClusterConfig,
	},
	{
		name:   "kubectl",
		create: newKubectlConfig,
	},
}

// NewRestConfig will attempt to create a new rest config from all configured options and return
// the first successfully created configuration.  The flag restConfigSource, if specified, will
// change the behavior to attempt to create from only the configured source.
func NewRestConfig(timeout time.Duration) (*rest.Config, error) {
	var errorStrs []string

	for _, source := range configSources {
		config, err := source.create()
		if err == nil {
			klog.V(1).Infof("Created rest config from source %s", source.name)
			klog.V(7).Infof("Config: %#v", *config)
			// Comfortable QPS limits for us to run smoothly.
			// The defaults are too low. It is probably safe to increase these if
			// we see problems in the future or need to accommodate VERY large numbers
			// of resources.
			//
			// config.QPS does not apply to WATCH requests. Currently, there is no client-side
			// or server-side rate-limiting for WATCH requests.
			config.QPS = 20
			config.Burst = 40
			config.Timeout = timeout
			return config, nil
		}

		klog.V(5).Infof("Failed to create from %s: %s", source.name, err)
		errorStrs = append(errorStrs, fmt.Sprintf("%s: %s", source.name, err))
	}

	return nil, errors.Errorf("Unable to create rest config:\n%s", strings.Join(errorStrs, "\n"))
}

// NewConfigFlags builds ConfigFlags based on an existing rest config.
// Burst QPS is increased by 3x for discovery.
// CacheDir is populated from the KUBECACHEDIR env var, if set.
func NewConfigFlags(config *rest.Config) (*genericclioptions.ConfigFlags, error) {
	cf := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()

	// Build default rest.Config
	cfg, err := cf.ToRESTConfig()
	if err != nil {
		return cf, fmt.Errorf("failed to build rest config: %w", err)
	}

	// Modify the new persistent rest.Config to match the provided one
	restConfigDeepCopyInto(config, cfg)

	cf = cf.WithDiscoveryQPS(cfg.QPS)
	if cfg.Burst > 0 {
		cf = cf.WithDiscoveryBurst(cfg.Burst * 3)
	}

	// Optionally override default CacheDir ($HOME/.kube/cache)
	// https://github.com/kubernetes/kubernetes/pull/109479
	envPath := os.Getenv("KUBECACHEDIR")
	if envPath != "" {
		cf.CacheDir = &envPath
	}

	return cf, nil
}

// restConfigDeepCopyInto copies one rest.Config into another.
// For reference, see rest.CopyConfig:
// https://github.com/kubernetes/client-go/blob/v0.24.0/rest/config.go#L630
func restConfigDeepCopyInto(from, to *rest.Config) {
	to.Host = from.Host
	to.APIPath = from.APIPath
	to.ContentConfig = from.ContentConfig
	to.Username = from.Username
	to.Password = from.Password
	to.BearerToken = from.BearerToken
	to.BearerTokenFile = from.BearerTokenFile
	to.Impersonate = rest.ImpersonationConfig{
		UserName: from.Impersonate.UserName,
		UID:      from.Impersonate.UID,
		Groups:   from.Impersonate.Groups,
		Extra:    from.Impersonate.Extra,
	}
	to.AuthProvider = from.AuthProvider
	to.AuthConfigPersister = from.AuthConfigPersister
	to.ExecProvider = from.ExecProvider
	to.TLSClientConfig = rest.TLSClientConfig{
		Insecure:   from.TLSClientConfig.Insecure,
		ServerName: from.TLSClientConfig.ServerName,
		CertFile:   from.TLSClientConfig.CertFile,
		KeyFile:    from.TLSClientConfig.KeyFile,
		CAFile:     from.TLSClientConfig.CAFile,
		CertData:   from.TLSClientConfig.CertData,
		KeyData:    from.TLSClientConfig.KeyData,
		CAData:     from.TLSClientConfig.CAData,
		NextProtos: from.TLSClientConfig.NextProtos,
	}
	to.UserAgent = from.UserAgent
	to.DisableCompression = from.DisableCompression
	to.Transport = from.Transport
	to.WrapTransport = from.WrapTransport
	to.QPS = from.QPS
	to.Burst = from.Burst
	to.RateLimiter = from.RateLimiter
	to.WarningHandler = from.WarningHandler
	to.Timeout = from.Timeout
	to.Dial = from.Dial
	to.Proxy = from.Proxy
	if from.ExecProvider != nil && from.ExecProvider.Config != nil {
		to.ExecProvider.Config = from.ExecProvider.Config.DeepCopyObject()
	}
}
