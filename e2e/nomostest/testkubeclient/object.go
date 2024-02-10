// Copyright 2024 Google LLC
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

package testkubeclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	jserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SerializeObject converts the object into bytes based on the provided format
func SerializeObject(obj client.Object, ext string, scheme *runtime.Scheme) ([]byte, error) {
	// We must convert through JSON/Unstructured to avoid "omitempty" fields
	// from being specified.
	uObj, err := reconcile.AsUnstructuredSanitized(obj)
	if err != nil {
		return nil, fmt.Errorf("sanitizing object: %w", err)
	}
	switch ext {
	case ".yaml", ".yml":
		// Encode converts to json then yaml, to handle "omitempty" directives.
		yamlSerializer := jserializer.NewSerializerWithOptions(
			jserializer.DefaultMetaFactory, scheme, scheme,
			jserializer.SerializerOptions{Yaml: true},
		)
		bytes, err := runtime.Encode(yamlSerializer, uObj)
		if err != nil {
			return nil, fmt.Errorf("encoding object as yaml: %w", err)
		}
		return bytes, nil
	case ".json":
		bytes, err := json.MarshalIndent(uObj, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encoding object as json: %w", err)
		}
		return bytes, nil
	default:
		// If you're seeing this error, use "AddFile" instead to test ignoring
		// files with extensions we ignore.
		return nil, fmt.Errorf("invalid extension to write object to, %q, use .AddFile() instead", ext)
	}
}

// WriteToFile writes the bytes to the provided path.
func WriteToFile(absPath string, bytes []byte) error {
	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
		return fmt.Errorf("creating directory %s: %w", parentDir, err)
	}

	// Write bytes to file.
	if err := os.WriteFile(absPath, bytes, os.ModePerm); err != nil {
		return fmt.Errorf("writing file %s: %w", absPath, err)
	}
	return nil
}
