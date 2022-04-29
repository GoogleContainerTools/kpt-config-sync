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
	"strings"
	"time"

	"github.com/pkg/errors"
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
