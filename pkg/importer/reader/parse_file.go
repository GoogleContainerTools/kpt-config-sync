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

package reader

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
)

// yamlWhitespace records the two valid YAML whitespace characters.
const yamlWhitespace = " \t"

func parseFile(path string) ([]*unstructured.Unstructured, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("attempted to read relative path")
	}

	switch filepath.Ext(path) {
	case ".yml", ".yaml":
		contents, err := ioutil.ReadFile(path)
		if err != nil {
			klog.Errorf("Failed to read file declared in git from mounted filesystem: %s", path)
			importer.Metrics.Violations.Inc()
			return nil, err
		}
		return parseYAMLFile(contents)
	case ".json":
		contents, err := ioutil.ReadFile(path)
		if err != nil {
			klog.Errorf("Failed to read file declared in git from mounted filesystem: %s", path)
			importer.Metrics.Violations.Inc()
			return nil, err
		}
		return parseJSONFile(contents)
	default:
		return nil, nil
	}
}

func isEmptyYAMLDocument(document string) bool {
	lines := strings.Split(document, "\n")
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, yamlWhitespace)
		if len(trimmed) == 0 || strings.HasPrefix(trimmed, "#") {
			// Ignore empty/whitespace-only/comment lines.
			continue
		}
		return false
	}
	return true
}

func parseYAMLFile(contents []byte) ([]*unstructured.Unstructured, error) {
	// We have to manually split documents with the YAML separator since by default
	// yaml.Unmarshal only unmarshalls the first document, but a file may contain multiple.
	var result []*unstructured.Unstructured

	// A newline followed by triple-dash begins a new YAML document, so this is safe.
	documents := strings.Split(string(contents), "\n---")
	for _, document := range documents {
		if isEmptyYAMLDocument(document) {
			// Kubernetes ignores empty documents.
			continue
		}

		var u unstructured.Unstructured
		_, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(document), nil, &u)
		if err != nil {
			return nil, err
		}
		result = append(result, &u)
	}
	return filterLocalConfigUnstructured(result), nil
}

func parseJSONFile(contents []byte) ([]*unstructured.Unstructured, error) {
	if len(contents) == 0 {
		// While an empty files is not valid JSON, Kubernetes allows empty JSON
		// files when applying multiple files.
		return nil, nil
	}
	// Kubernetes does not recognize arrays of Kubernetes objects in JSON files.
	// A single file must contain exactly one Kubernetes object, so we don't
	// have to do the same work we had to do for YAML.
	var u unstructured.Unstructured
	err := u.UnmarshalJSON(contents)
	if err != nil {
		return nil, err
	}
	return filterLocalConfigUnstructured([]*unstructured.Unstructured{&u}), nil
}

func parseKptfile(contents []byte) ([]*unstructured.Unstructured, error) {
	unstructs, err := parseYAMLFile(contents)
	if err != nil {
		return nil, err
	}
	switch len(unstructs) {
	case 0:
		return nil, nil
	case 1:
		if unstructs[0].GroupVersionKind().GroupKind() != kinds.KptFile().GroupKind() {
			return nil, fmt.Errorf("only one resource of type Kptfile allowed in Kptfile")
		}
		return unstructs, nil
	default:
		return nil, fmt.Errorf("only one resource of type Kptfile allowed in Kptfile")
	}
}

func hasStatusField(u runtime.Unstructured) bool {
	// The following call will only error out if the UnstructuredContent returns something that is not a map.
	// This has already been verified upstream.
	m, ok, err := unstructured.NestedFieldNoCopy(u.UnstructuredContent(), "status")
	if err != nil {
		// This should never happen!!!
		klog.Errorf("unexpected error retrieving status field: %v:\n%v", err, u)
	}
	return ok && m != nil && len(m.(map[string]interface{})) != 0
}

func filterLocalConfigUnstructured(unstructureds []*unstructured.Unstructured) []*unstructured.Unstructured {
	var result []*unstructured.Unstructured
	for _, u := range unstructureds {
		if core.GetAnnotation(u, metadata.LocalConfigAnnotationKey) == metadata.LocalConfigValue {
			continue
		}
		result = append(result, u)
	}
	return result
}
