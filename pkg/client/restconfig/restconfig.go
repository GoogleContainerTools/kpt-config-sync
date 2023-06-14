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
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // kubectl auth provider plugins
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/flowcontrol"
)

// DefaultTimeout is the default REST config timeout.
const DefaultTimeout = 15 * time.Second

// NewRestConfig will attempt to create a new rest config from all configured
// options and return the first successfully created configuration.
// The client-side throttling is only determined by if server-side flow control
// is enabled or not. If server-side flow control is enabled, then client-side
// throttling is disabled, vice versa.
func NewRestConfig(timeout time.Duration) (*rest.Config, error) {
	var errorStrs []string

	// Build from k8s downward API
	config, err := NewFromInClusterConfig()
	if err != nil {
		errorStrs = append(errorStrs, fmt.Sprintf("reading in-cluster config: %s", err))

		// Detect path from env var
		path, err := newConfigPath()
		if err != nil {
			errorStrs = append(errorStrs, fmt.Sprintf("finding local kubeconfig: %s", err))
		} else {
			// Build from local config file
			config, err = NewFromConfigFile(path)
			if err != nil {
				errorStrs = append(errorStrs, fmt.Sprintf("reading local kubeconfig: %s", err))
			} else {
				errorStrs = nil
			}
		}
	}
	if len(errorStrs) > 0 {
		return nil, fmt.Errorf("failed to build rest config:\n%s", strings.Join(errorStrs, "\n"))
	}
	// Set timeout, if specified.
	if timeout != 0 {
		config.Timeout = timeout
	}
	klog.V(7).Infof("Config: %#v", *config)
	return config, nil
}

// NewFromConfigFile returns a REST config built from the kube config file at
// the specified path.
func NewFromConfigFile(path string) (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, errors.Wrapf(err, "loading REST config from %q", path)
	}
	setDefaults(config)
	return config, nil
}

// NewFromInClusterConfig returns a REST config built from the k8s downward API.
// This should work from inside a Pod to talk to the cluster the Pod is in.
func NewFromInClusterConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "loading REST config from K8s downward API")
	}
	setDefaults(config)
	return config, nil
}

func setDefaults(config *rest.Config) {
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	UpdateQPS(config)
}

// UpdateQPS modifies a rest.Config to update the client-side throttling QPS and
// Burst QPS.
//
// - If Flow Control is enabled on the apiserver, and client-side throttling is
// not forced to be enabled, client-side throttling is disabled!
// - If Flow Control is disabled or undetected on the apiserver, client-side
// throttling QPS will be increased to at least 30 (burst: 60).
//
// Flow Control is enabled by default on Kubernetes v1.20+.
// https://kubernetes.io/docs/concepts/cluster-administration/flow-control/
func UpdateQPS(config *rest.Config) {
	// Timeout if the query takes too long, defaulting to the lower QPS limits.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	flowControlEnabled, err := flowcontrol.IsEnabled(ctx, config)
	if err != nil {
		klog.Warning("Failed to query apiserver to check for flow control enablement: %v", err)
		// Default to the lower QPS limits.
	}
	if flowControlEnabled {
		config.QPS = -1
		config.Burst = -1
		klog.V(1).Infof("Flow control enabled on apiserver: client-side throttling QPS set to %.0f (burst: %d)", config.QPS, config.Burst)
	} else {
		config.QPS = maxIfNotNegative(config.QPS, 30)
		config.Burst = int(maxIfNotNegative(float32(config.Burst), 60))
		klog.V(1).Infof("Flow control disabled on apiserver: client-side throttling QPS set to %.0f (burst: %d)", config.QPS, config.Burst)
	}
}

func maxIfNotNegative(a, b float32) float32 {
	switch {
	case a < 0:
		return a
	case a > b:
		return a
	default:
		return b
	}
}

// NewConfigFlags builds ConfigFlags based on an existing rest config.
// Burst QPS is increased by 3x for discovery.
// CacheDir is populated from the KUBECACHEDIR env var, if set.
func NewConfigFlags(config *rest.Config) (*genericclioptions.ConfigFlags, error) {
	// New non-interactive config (no password flags or prompt).
	// Delay initialization (reading KUBECONFIG) until first ToRESTConfig() call.
	// Persist rest.Config after initialization.
	cf := genericclioptions.NewConfigFlags(true)

	// Copy the pointer from the stack to the heap so we can use it in the closure after this function has returned.
	configPtrCopy := config
	// Modify the rest.Config after initialization by copying from the supplied config.
	cf.WrapConfigFn = func(factoryCfg *rest.Config) *rest.Config {
		DeepCopyInto(configPtrCopy, factoryCfg)
		return factoryCfg
	}

	// Use the same QPS for discovery.
	cf = cf.WithDiscoveryQPS(config.QPS)
	// Use a higher burst QPS for discovery, if not unlimitted.
	if config.Burst > 0 {
		cf = cf.WithDiscoveryBurst(config.Burst * 3)
	}

	// Optionally override default CacheDir ($HOME/.kube/cache)
	// https://github.com/kubernetes/kubernetes/pull/109479
	envPath := os.Getenv("KUBECACHEDIR")
	if envPath != "" {
		cf.CacheDir = &envPath
	}

	return cf, nil
}

// DeepCopyInto copies one rest.Config into another.
// For reference, see rest.CopyConfig:
// https://github.com/kubernetes/client-go/blob/v0.24.0/rest/config.go#L630
func DeepCopyInto(from, to *rest.Config) {
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

// DeepCopy returns a deep copy of the specified rest.Config.
// For reference, see rest.CopyConfig:
// https://github.com/kubernetes/client-go/blob/v0.24.0/rest/config.go#L630
func DeepCopy(from *rest.Config) *rest.Config {
	to := &rest.Config{}
	DeepCopyInto(from, to)
	return to
}
