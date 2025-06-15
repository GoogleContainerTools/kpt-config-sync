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

package vet

import (
	"context"
	"io"
	"time"

	"kpt.dev/configsync/pkg/api/configsync"
)

// VetExecutionParams contains all parameters needed to execute the vet command
// This struct is completely independent of cobra command structures
type VetExecutionParams struct {
	Clusters         []string
	Path             string
	SkipAPIServer    bool
	SourceFormat     configsync.SourceFormat
	OutputFormat     string
	APIServerTimeout time.Duration
	Namespace        string
	KeepOutput       bool
	MaxObjectCount   int
	OutPath          string
}

// ExecuteVet executes the core vet command logic without any cobra dependencies
// This function encapsulates all the business logic for the vet command
func ExecuteVet(ctx context.Context, out io.Writer, params VetExecutionParams) error {
	// Create vet options from execution parameters
	// This separates the cobra command structure from the actual business logic
	opts := vetOptions{
		Namespace:        params.Namespace,
		SourceFormat:     params.SourceFormat,
		APIServerTimeout: params.APIServerTimeout,
		MaxObjectCount:   params.MaxObjectCount,
		KeepOutput:       params.KeepOutput,
		OutPath:          params.OutPath,
		OutputFormat:     params.OutputFormat,
	}

	// Execute the actual vet logic
	return runVet(ctx, out, opts)
}
