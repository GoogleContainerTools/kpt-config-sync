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
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/metadata"
)

func parseFile(path string) ([]*unstructured.Unstructured, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("attempted to read relative path")
	}

	switch filepath.Ext(path) {
	case ".yml", ".yaml":
		contents, err := os.ReadFile(path)
		if err != nil {
			klog.Errorf("Failed to read file declared in git from mounted filesystem: %s", path)
			// TODO: This uses the old importer metric. Should this be changed?
			importer.Metrics.Violations.Inc()
			return nil, err
		}
		return parseYAMLFile(contents)
	case ".json":
		contents, err := os.ReadFile(path)
		if err != nil {
			klog.Errorf("Failed to read file declared in git from mounted filesystem: %s", path)
			// TODO: This uses the old importer metric. Should this be changed?
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
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		if len(trimmed) == 0 || strings.HasPrefix(trimmed, "#") {
			// Ignore empty/whitespace-only/comment lines.
			continue
		}
		return false
	}
	return true
}

// parseYAMLFile parses a byte array as a YAML document stream.
// Each document, if not empty, is decoded into an Unstructured object.
//
// According to the YAML spec, `---` is the directive end marker, indicating the
// end of directives and the beginning of a document. Kubernetes does not
// support YAML directives, so we're not supporting them here either. But we do
// support directive end markers (`---`) and document end markers (`...`).
//
// Since the yaml decoder handles stopping at document end markers, we only need
// to explicitly handle directive end markers as delimiters here.
//
// While not technically to spec, this parser also ignores empty documents, to
// avoid erroring in cases where kubectl apply would work. This also helps
// avoid errors when using Helm to produce YAML using loops and conditionals.
//
// YAML Spec for document streams:
// https://yaml.org/spec/1.2.2/#document-stream-productions
func parseYAMLFile(contents []byte) ([]*unstructured.Unstructured, error) {
	// We have to manually split documents with the YAML separator since by default
	// yaml.Unmarshal only unmarshalls the first document, but a file may contain multiple.
	var result []*unstructured.Unstructured

	// Split on directive end markers.
	// Prepend line break to ensure leading directive end markers are handled.
	documents := strings.Split("\n"+string(contents), "\n---")
	for _, document := range documents {
		// Ignore empty documents
		if isEmptyYAMLDocument(document) {
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
		annoVal, found := u.GetAnnotations()[metadata.LocalConfigAnnotationKey]
		if found && annoVal != metadata.NoLocalConfigAnnoVal {
			continue
		}
		result = append(result, u)
	}
	return result
}
