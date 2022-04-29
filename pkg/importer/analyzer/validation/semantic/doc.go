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

// Package semantic package provides validation checks for semantic errors in Nomos resource
// directories.
//
// For the purpose of this package, we define a "semantic error" to be a configuration error which
// cannot be determined by looking at a single Resource. Examples of semantic errors include
// detecting duplicate directories and verifying that a NamespaceSelector references a Namespace
// that exists.
package semantic
