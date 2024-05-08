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

package fake

import (
	"fmt"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
)

// ConfigParser fakes filesystem.ConfigParser.
//
// This is not in kpt.dev/configsync/pkg/testing/fake because that would cause
// a import loop (filesystem -> fake -> filesystem).
type ConfigParser struct {
	Calls   int
	Inputs  []ParserInputs
	Outputs []ParserOutputs
}

// ParserInputs stores inputs for fake.ConfigParser.Parse()
type ParserInputs struct {
	FilePaths reader.FilePaths
}

// ParserOutputs stores outputs for fake.ConfigParser.Parse()
type ParserOutputs struct {
	FileObjects []ast.FileObject
	Errors      status.MultiError
}

// Parse fakes filesystem.ConfigParser.Parse
func (p *ConfigParser) Parse(filePaths reader.FilePaths) ([]ast.FileObject, status.MultiError) {
	p.Inputs = append(p.Inputs, ParserInputs{
		FilePaths: filePaths,
	})
	if p.Calls >= len(p.Outputs) {
		panic(fmt.Sprintf("Expected only %d calls to ConfigParser.Parse, but got more. Update ConfigParser.Outputs if this is expected.", len(p.Outputs)))
	}
	outputs := p.Outputs[p.Calls]
	p.Calls++
	return outputs.FileObjects, outputs.Errors
}

// ReadClusterRegistryResources fakes filesystem.ConfigParser.ReadClusterRegistryResources
func (p *ConfigParser) ReadClusterRegistryResources(_ reader.FilePaths, _ filesystem.SourceFormat) ([]ast.FileObject, status.MultiError) {
	return nil, nil
}

// ReadClusterNamesFromSelector fakes filesystem.ConfigParser.ReadClusterNamesFromSelector
func (p *ConfigParser) ReadClusterNamesFromSelector(_ reader.FilePaths) ([]string, status.MultiError) {
	return nil, nil
}

var _ filesystem.ConfigParser = &ConfigParser{}
