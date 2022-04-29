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

package fake

import (
	v1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
)

// NamespaceObject returns an initialized Namespace.
func NamespaceObject(name string, opts ...core.MetaMutator) *v1.Namespace {
	result := &v1.Namespace{TypeMeta: ToTypeMeta(kinds.Namespace())}
	defaultMutate(result)
	mutate(result, core.Name(name))
	mutate(result, opts...)

	return result
}

// Namespace returns a Namespace FileObject with the passed opts.
//
// namespaceDir is the directory path within namespaces/ to the Namespace. Parses
//   namespacesDir to determine valid default metadata.Name.
func Namespace(dir string, opts ...core.MetaMutator) ast.FileObject {
	relative := cmpath.RelativeSlash(dir).Join(cmpath.RelativeSlash("namespace.yaml"))
	return NamespaceAtPath(relative.SlashPath(), opts...)
}

// NamespaceAtPath returns a Namespace at exactly the passed path.
func NamespaceAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	name := cmpath.RelativeSlash(path).Dir().Base()
	return FileObject(NamespaceObject(name, opts...), path)
}
