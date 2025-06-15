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

package bugreport

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"kpt.dev/configsync/pkg/bugreport"
	"kpt.dev/configsync/pkg/client/restconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExecParams contains all parameters needed to execute the bugreport command
// This struct is completely independent of cobra command structures
type ExecParams struct {
	ClientTimeout time.Duration
}

// ExecuteBugreport executes the core bugreport command logic without any cobra dependencies
// This function encapsulates all the business logic for the bugreport command
func ExecuteBugreport(ctx context.Context, params ExecParams) error {

	cfg, err := restconfig.NewRestConfig(params.ClientTimeout)
	if err != nil {
		return fmt.Errorf("failed to create rest config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client set: %w", err)
	}
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	report, err := bugreport.New(ctx, c, cs)
	if err != nil {
		return fmt.Errorf("failed to initialize bug reporter: %w", err)
	}

	if err = report.Open(); err != nil {
		return err
	}

	report.WriteRawInZip(report.FetchLogSources(ctx))
	report.WriteRawInZip(report.FetchResources(ctx))
	report.WriteRawInZip(report.FetchCMSystemPods(ctx))
	report.AddNomosStatusToZip(ctx)
	report.AddNomosVersionToZip(ctx)

	report.Close()
	return nil
}
