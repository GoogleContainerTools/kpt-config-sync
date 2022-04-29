/*
Copyright 2021 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kmetrics

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// RunKustomizeBuild runs `kustomize build` with the provided arguments.
// inputDir is the directory upon which to invoke `kustomize build`, and
// flags is what flags to run on it.
//
// For example: RunKustomizeBuild(".", []string{"--enable-helm"}) will run
// `kustomize build . --enable-helm`.
//
// The argument sendMetrics determines whether to send metrics about kustomize
// to Google Cloud.
//
// Prior to running `kustomize build`, the wrapper will check the `buildMetadata`
// field of the kustomization file. By default, we would like to enable the
// `buildMetadata.originAnnotations`. If the kustomization file does not include
// it already, we add it and remove it afterwards.
func RunKustomizeBuild(ctx context.Context, sendMetrics bool, inputDir string, flags ...string) (string, error) {
	args := []string{"build", inputDir}
	args = append(args, flags...)

	var kustPath string
	var b []byte
	err := func() error {
		b, kustPath = readKustomizeFileBytes(inputDir)
		if b == nil {
			return fmt.Errorf("Error: unable to find one of 'kustomization.yaml', 'kustomization.yml' or 'Kustomization' in directory")
		}

		var kt *types.Kustomization
		if err := yaml.Unmarshal(b, &kt); err != nil {
			// The error message here is very unpleasant and confusing. We will get a better error
			// message from running `kustomize build` below if we ignore the error here.
			kt = nil
		}

		if kt != nil {
			hasOriginAnno := false
			for _, opt := range kt.BuildMetadata {
				if opt == types.OriginAnnotations {
					hasOriginAnno = true
					break
				}
			}
			if !hasOriginAnno {
				cmd := exec.Command("kustomize", "edit", "add", "buildmetadata", types.OriginAnnotations)
				cmd.Dir = inputDir
				_, _, err := runCommand(cmd)
				if err != nil {
					return err
				}

			}
		}

		return nil
	}()

	if err != nil {
		// TODO: Export this error to the metrics dashboard.
		log.Printf("error setting originAnnotations: %v\n", err)
	}

	defer func() {
		if b != nil {
			// we write back the original kustomization file contents (rather than simply doing `kustomize edit remove
			// buildmetadata originAnnotations`), because `kustomize edit` can cause some undesired formatting change
			_ = os.WriteFile(kustPath, b, 0)
		}
	}()

	cmd := exec.Command("kustomize", args...)
	out, buildErr := runKustomizeBuild(ctx, sendMetrics, inputDir, cmd)

	return out, buildErr
}

// runKustomizeBuild will run `kustomize build` and also record measurements
// about kustomize usage via OpenCensus. This assumes that there is already an OC
// agent that is sending to a collector.
func runKustomizeBuild(ctx context.Context, sendMetrics bool, inputDir string, cmd *exec.Cmd) (string, error) {
	var wg sync.WaitGroup
	outputs := make(chan string, 1)
	errors := make(chan error, 1)
	wg.Add(1)

	go func() {
		executionTime, output, kustomizeErr := runCommand(cmd)
		// Send execution time and resource count metrics to OC collector
		resourceCount, err := kustomizeResourcesGenerated(output)
		if err == nil && sendMetrics {
			RecordKustomizeResourceCount(ctx, resourceCount)
			RecordKustomizeExecutionTime(ctx, float64(executionTime))
		}
		outputs <- output
		errors <- kustomizeErr
		wg.Done()
	}()

	kt, err := readKustomizeFile(inputDir)
	if kt != nil && err == nil {
		fieldMetrics, fieldErr := kustomizeFieldUsage(kt, inputDir)
		if fieldErr == nil && fieldMetrics != nil && sendMetrics {
			// Send field count metrics to OC collector
			RecordKustomizeFieldCountData(ctx, fieldMetrics)
		}
	}
	wg.Wait()
	return <-outputs, <-errors
}

func runCommand(cmd *exec.Cmd) (int64, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	now := time.Now()
	err := cmd.Run()
	executionTime := time.Since(now).Nanoseconds()
	if err != nil {
		return executionTime, stdout.String(), fmt.Errorf(stderr.String())
	}
	return executionTime, stdout.String(), nil
}
