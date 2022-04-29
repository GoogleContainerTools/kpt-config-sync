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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/repo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RepoVersion sets the Spec.Version of a Repo.
func RepoVersion(version string) core.MetaMutator {
	return func(f client.Object) {
		f.(*v1.Repo).Spec.Version = version
	}
}

// RepoObject returns an initialized Repo.
func RepoObject(opts ...core.MetaMutator) *v1.Repo {
	result := &v1.Repo{TypeMeta: ToTypeMeta(kinds.Repo())}
	defaultMutate(result)
	mutate(result, core.Name("repo"))
	RepoVersion(repo.CurrentVersion)(result)
	for _, opt := range opts {
		opt(result)
	}

	return result
}

// Repo returns a default Repo with sensible defaults.
func Repo(opts ...core.MetaMutator) ast.FileObject {
	return RepoAtPath("system/repo.yaml", opts...)
}

// RepoAtPath returns a Repo at a specified path.
func RepoAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(RepoObject(opts...), path)
}
