// Copyright 2025 Google LLC
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
	"os"
	"time"

	"k8s.io/client-go/rest"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// ExecParams contains all parameters needed to execute the version command
// This struct is completely independent of cobra command structures
type ExecParams struct {
	Contexts      []string
	ClientTimeout time.Duration
}

// ExecuteVersion executes the core version command logic without any cobra dependencies
// This function encapsulates all the business logic for the version command
func ExecuteVersion(ctx context.Context, params ExecParams) error {
	allCfgs, err := getAllKubectlConfigs(params.Contexts, params.ClientTimeout)
	versionInternal(ctx, allCfgs, os.Stdout)

	if err != nil {
		return fmt.Errorf("unable to parse kubectl config: %w", err)
	}
	return nil
}

// getAllKubectlConfigs gets all kubectl configs, with error handling
// This function is extracted to remove dependency on global flags
func getAllKubectlConfigs(contexts []string, clientTimeout time.Duration) (map[string]*rest.Config, error) {
	allCfgs, err := restconfig.AllKubectlConfigs(clientTimeout, contexts)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr
		}

		fmt.Printf("failed to create client configs: %v\n", err)
	}

	return allCfgs, err
}
