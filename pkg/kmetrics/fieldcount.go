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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"sigs.k8s.io/kustomize/api/hasher"
	"sigs.k8s.io/kustomize/api/konfig"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// KustomizeFieldMetrics stores kustomize metrics
type KustomizeFieldMetrics struct {
	// fieldCount is how many times a given field is used in an instance
	// of kustomize build (including in bases and overlays)
	FieldCount map[string]int

	// TopTierCount looks at the fields Resources, Generators, SecretGenerator,
	// ConfigMapGenerator, Transformers, and Validators, and counts how many
	// of each are being used in a kustomization, e.g. for resources, how
	// many total resource paths there are.
	TopTierCount map[string]int

	// PatchCount looks at the patches, patchesStrategicMerge, and patchesJson6902
	// and counts how many patches are in each field
	PatchCount map[string]int

	// BaseCount counts the number of remote and local bases
	BaseCount map[string]int

	// HelmMetrics counts the usage of helm in kustomize, if the new helm generator
	// function is used and how many charts are being inflated with the builtin
	// helm fields
	HelmMetrics map[string]int

	// K8sMetadata looks at the usage of builtin transformers related to kubernetes
	// object metadata
	K8sMetadata map[string]int

	// SimplMetrics looks at the usage of simplification transformers images, replicas,
	// and replacements
	SimplMetrics map[string]int

	// DeprecationMetrics looks at the usage of fields that may become deprecated
	DeprecationMetrics map[string]int
}

// RenderHelmChart is the name of the KRM function that inflates a helm chart
const RenderHelmChart = "render-helm-chart"

// kustomizeFieldUsage reads the current kustomization file (and any
// base kustomization file it refers to) to gather metrics about
// the usages of fields. It returns a map of field names -> the number
// of times that field is used in the kustomization stack. This data
// will be sent via opentelemetry to cloud monitoring.
func kustomizeFieldUsage(kt *types.Kustomization, path string) (*KustomizeFieldMetrics, error) {
	if kt == nil {
		return nil, nil
	}
	return kustomizeFieldUsageRecurse(kt, path)
}

func readKustomizeFile(path string) (*types.Kustomization, error) {
	for _, f := range konfig.RecognizedKustomizationFileNames() {
		b, err := os.ReadFile(filepath.Join(path, f))
		if err == nil {
			kt := &types.Kustomization{}
			if err := yaml.Unmarshal(b, kt); err != nil {
				return nil, fmt.Errorf("error reading kustomization file in directory %s:%w", path, err)
			}
			return kt, nil
		}
	}
	return nil, nil
}

func readKustomizeFileBytes(path string) ([]byte, string) {
	for _, f := range konfig.RecognizedKustomizationFileNames() {
		kustPath := filepath.Join(path, f)
		b, err := os.ReadFile(kustPath)
		if err == nil {
			return b, kustPath
		}
	}
	return nil, ""
}

func kustomizeFieldUsageRecurse(k *types.Kustomization, path string) (*KustomizeFieldMetrics, error) {
	fieldCount := make(map[string]int)
	topTierCount := make(map[string]int)
	patchCount := make(map[string]int)
	baseCount := make(map[string]int)
	helmMetrics := make(map[string]int)
	k8sMetadata := make(map[string]int)
	simplMetrics := make(map[string]int)
	deprecationMetrics := make(map[string]int)

	subDirs := append(k.Resources, append(k.Bases, k.Components...)...)
	localBases := 0
	for i, r := range subDirs {
		// try to read the resource as a base/directory to get the base kustomization fields
		basePath := filepath.Join(path, r)
		files, err := os.ReadDir(basePath)
		if err == nil && len(files) > 0 {
			if i < len(k.Resources)+len(k.Bases) {
				localBases++
			}
			subKt, err := readKustomizeFile(basePath)
			if err != nil {
				return nil, err
			}
			if subKt != nil {
				subKtMetrics, err := kustomizeFieldUsageRecurse(subKt, basePath)
				if err != nil {
					return nil, err
				}
				fieldCount = aggregateMapCounts(fieldCount, subKtMetrics.FieldCount)
				topTierCount = aggregateMapCounts(topTierCount, subKtMetrics.TopTierCount)
				patchCount = aggregateMapCounts(patchCount, subKtMetrics.PatchCount)
				baseCount = aggregateMapCounts(baseCount, subKtMetrics.BaseCount)
				helmMetrics = aggregateMapCounts(helmMetrics, subKtMetrics.HelmMetrics)
				k8sMetadata = aggregateMapCounts(k8sMetadata, subKtMetrics.K8sMetadata)
				simplMetrics = aggregateMapCounts(simplMetrics, subKtMetrics.SimplMetrics)
				deprecationMetrics = aggregateMapCounts(deprecationMetrics, subKtMetrics.DeprecationMetrics)
			}
		}
	}
	fieldCount = aggregateMapCounts(fieldCount, kustomizationFieldCount(k))
	topTierCount = aggregateMapCounts(topTierCount, orderedTopTierFieldCount(k))
	patchCount = aggregateMapCounts(patchCount, patchTypeCount(k))
	baseCount = aggregateMapCounts(baseCount, baseTypeCount(k, localBases))
	helmMetrics = aggregateMapCounts(helmMetrics, helmCount(k, path))
	k8sMetadata = aggregateMapCounts(k8sMetadata, k8sMetadataCount(k))
	simplMetrics = aggregateMapCounts(simplMetrics, simplificationUsage(k, path))
	deprecationMetrics = aggregateMapCounts(deprecationMetrics, deprecatedFieldCount(k))

	return &KustomizeFieldMetrics{
		FieldCount:         fieldCount,
		TopTierCount:       topTierCount,
		PatchCount:         patchCount,
		BaseCount:          baseCount,
		HelmMetrics:        helmMetrics,
		K8sMetadata:        k8sMetadata,
		SimplMetrics:       simplMetrics,
		DeprecationMetrics: deprecationMetrics,
	}, nil
}

func kustomizationFieldCount(k *types.Kustomization) map[string]int {
	result := make(map[string]int)
	v := reflect.ValueOf(*k)
	typeOfV := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsZero() {
			fieldName := typeOfV.Field(i).Name
			result[fieldName] = 1
		}
	}
	return result
}

func orderedTopTierFieldCount(k *types.Kustomization) map[string]int {
	if k == nil {
		return nil
	}
	result := make(map[string]int)
	if len(k.Resources) > 0 {
		result["Resources"] = len(k.Resources)
	}
	if len(k.Generators) > 0 {
		result["Generators"] = len(k.Generators)
	}
	if len(k.SecretGenerator) > 0 {
		result["SecretGenerators"] = len(k.SecretGenerator)
	}
	if len(k.ConfigMapGenerator) > 0 {
		result["ConfigMapGenerators"] = len(k.ConfigMapGenerator)
	}
	if len(k.Transformers) > 0 {
		result["Transformers"] = len(k.Transformers)
	}
	if len(k.Validators) > 0 {
		result["Validators"] = len(k.Validators)
	}
	return result
}

func patchTypeCount(k *types.Kustomization) map[string]int {
	if k == nil {
		return nil
	}
	result := make(map[string]int)
	if len(k.Patches) > 0 {
		result["Patches"] = len(k.Patches)
	}
	if len(k.PatchesStrategicMerge) > 0 {
		result["PatchesStrategicMerge"] = len(k.PatchesStrategicMerge)
	}
	if len(k.PatchesJson6902) > 0 {
		result["PatchesJson6902"] = len(k.PatchesJson6902)
	}
	return result
}

func baseTypeCount(k *types.Kustomization, localBases int) map[string]int {
	result := make(map[string]int)
	remoteBases := remoteBaseCount(k)
	if localBases > 0 {
		result["Local"] = localBases
	}
	if remoteBases > 0 {
		result["Remote"] = remoteBases
	}
	return result
}

func remoteBaseCount(k *types.Kustomization) int {
	result := 0
	for _, r := range k.Resources {
		origin := (&resource.Origin{}).Append(r)
		if origin.Repo != "" {
			result++
		}
	}
	return result
}

func helmCount(k *types.Kustomization, path string) map[string]int {
	result := make(map[string]int)
	for _, g := range k.Generators {
		contents, err := os.ReadFile(filepath.Join(path, g))
		if err != nil {
			contents = []byte(g)
		}
		if strings.Contains(string(contents), RenderHelmChart) {
			result = aggregateMapCounts(result, map[string]int{RenderHelmChart: 1})
		}
	}
	if len(k.HelmCharts) > 0 {
		result["HelmCharts"] = len(k.HelmCharts)
	}
	if len(k.HelmChartInflationGenerator) > 0 {
		result["HelmChartInflationGenerator"] = len(k.HelmChartInflationGenerator)
	}
	return result
}

func k8sMetadataCount(k *types.Kustomization) map[string]int {
	result := make(map[string]int)
	if k.NamePrefix != "" {
		result["NamePrefix"] = 1
	}
	if k.NameSuffix != "" {
		result["NameSuffix"] = 1
	}
	if k.Namespace != "" {
		result["Namespace"] = 1
	}
	if len(k.CommonLabels) > 0 {
		result["CommonLabels"] = len(k.CommonLabels)
	}
	if len(k.Labels) > 0 {
		numLabels := 0
		for i := range k.Labels {
			numLabels = numLabels + len(k.Labels[i].Pairs)
		}
		result["Labels"] = numLabels
	}
	if len(k.CommonAnnotations) > 0 {
		result["CommonAnnotations"] = len(k.CommonAnnotations)
	}
	return result
}

func simplificationUsage(k *types.Kustomization, path string) map[string]int {
	result := make(map[string]int)
	if len(k.Images) > 0 {
		result["Images"] = len(k.Images)
	}
	if len(k.Replicas) > 0 {
		result["Replicas"] = len(k.Replicas)
	}
	if len(k.Replacements) > 0 {
		result["ReplacementSources"] = len(k.Replacements)
		targets := 0
		for _, r := range k.Replacements {
			if r.Path != "" {
				bytes, err := os.ReadFile(filepath.Join(path, r.Path))
				if err != nil {
					continue
				}
				var p *types.Replacement
				if err := yaml.Unmarshal(bytes, &p); err != nil || p == nil {
					continue
				}
				targets = targets + len(p.Targets)

			} else {
				targets = targets + len(r.Targets)
			}
		}
		result["ReplacementTargets"] = targets
	}
	return result
}

func deprecatedFieldCount(k *types.Kustomization) map[string]int {
	result := make(map[string]int)
	if len(k.Vars) > 0 {
		result["Vars"] = len(k.Vars)
	}
	if len(k.Bases) > 0 {
		result["Bases"] = len(k.Bases)
	}
	if len(k.Crds) > 0 {
		result["Crds"] = len(k.Crds)
	}
	return result
}

// kustomizeResourcesGenerated returns the total number of resources produced
// in the output.
func kustomizeResourcesGenerated(output string) (int, error) {
	rf := resource.NewFactory(&hasher.Hasher{})
	resources, err := rf.SliceFromBytes([]byte(output))
	if err != nil {
		return 0, err
	}
	return len(resources), err
}

func aggregateMapCounts(m1 map[string]int, m2 map[string]int) map[string]int {
	for k, v2 := range m2 {
		if v1, ok := m1[k]; ok {
			m1[k] = v1 + v2
		} else {
			m1[k] = v2
		}
	}
	return m1
}
