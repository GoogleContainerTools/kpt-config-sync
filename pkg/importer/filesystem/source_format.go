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

package filesystem

// SourceFormat specifies how the Importer should parse the repository.
type SourceFormat string

// SourceFormatUnstructured says to parse all YAMLs in the config directory and
// ignore directory structure.
const SourceFormatUnstructured SourceFormat = "unstructured"

// SourceFormatHierarchy says to use hierarchical namespace inheritance based on
// directory structure and requires that manifests be declared in specific
// subdirectories.
const SourceFormatHierarchy SourceFormat = "hierarchy"

// SourceFormatKey is the OS env variable and ConfigMap key for the SOT
// repository format.
const SourceFormatKey = "SOURCE_FORMAT"
