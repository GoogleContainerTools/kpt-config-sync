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

package util

import (
	"os"
	"path/filepath"

	"k8s.io/cli-runtime/pkg/printers"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
)

// WriteObject writes a FileObject to a file using the provided ResourcePrinter.
// Writes to the file at object.OSPath(), overwriting if one exists.
func WriteObject(printer printers.ResourcePrinter, dir string, object ast.FileObject) error {
	if err := os.MkdirAll(filepath.Join(dir, object.Dir().OSPath()), 0750); err != nil {
		return err
	}

	file, err := os.Create(filepath.Join(dir, object.OSPath()))
	if err != nil {
		return err
	}

	return printer.PrintObj(object.Unstructured, file)
}
