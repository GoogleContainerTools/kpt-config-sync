// Copyright 2023 Google LLC
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

package artifactregistry

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	jserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WriteObjectYAMLFile formats the objects as YAML and writes it to the
// specified file. Creates parent directories as needed.
func WriteObjectYAMLFile(nt *nomostest.NT, path string, obj client.Object) error {
	fileMode := os.ModePerm
	yamlSerializer := jserializer.NewYAMLSerializer(jserializer.DefaultMetaFactory, nt.Scheme, nt.Scheme)
	var err error
	uObj, err := reconcile.AsUnstructuredSanitized(obj)
	if err != nil {
		return errors.Wrap(err, "sanitizing object")
	}
	// Encode converts to json then yaml, to handle "omitempty" directives.
	bytes, err := runtime.Encode(yamlSerializer, uObj)
	if err != nil {
		return errors.Wrap(err, "encoding object as yaml")
	}
	dirPath := filepath.Dir(path)
	if dirPath != "." {
		if err := os.MkdirAll(dirPath, fileMode); err != nil {
			return errors.Wrapf(err, "making parent directories: %s", dirPath)
		}
	}
	if err := os.WriteFile(path, bytes, fileMode); err != nil {
		return errors.Wrapf(err, "writing file: %s", path)
	}
	return nil
}
