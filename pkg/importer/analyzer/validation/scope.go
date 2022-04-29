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

package validation

import (
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IncorrectTopLevelDirectoryErrorCode is the error code for IllegalKindInClusterError
const IncorrectTopLevelDirectoryErrorCode = "1039"

var incorrectTopLevelDirectoryErrorBuilder = status.NewErrorBuilder(IncorrectTopLevelDirectoryErrorCode)

// ShouldBeInSystemError reports that an object belongs in system/.
func ShouldBeInSystemError(resource client.Object) status.Error {
	return incorrectTopLevelDirectoryErrorBuilder.
		Sprintf("Repo and HierarchyConfig configs MUST be declared in `%s/`. "+
			"To fix, move the %s to `%s/`.", repo.SystemDir, resource.GetObjectKind().GroupVersionKind().Kind, repo.SystemDir).
		BuildWithResources(resource)
}

// ShouldBeInClusterRegistryError reports that an object belongs in clusterregistry/.
func ShouldBeInClusterRegistryError(resource client.Object) status.Error {
	return incorrectTopLevelDirectoryErrorBuilder.
		Sprintf("Cluster and ClusterSelector configs MUST be declared in `%s/`. "+
			"To fix, move the %s to `%s/`.", repo.ClusterRegistryDir, resource.GetObjectKind().GroupVersionKind().Kind, repo.ClusterRegistryDir).
		BuildWithResources(resource)
}

// ShouldBeInClusterError reports that an object belongs in cluster/.
func ShouldBeInClusterError(resource client.Object) status.Error {
	return incorrectTopLevelDirectoryErrorBuilder.
		Sprintf("Cluster-scoped configs except Namespaces MUST be declared in `%s/`. "+
			"To fix, move the %s to `%s/`.", repo.ClusterDir, resource.GetObjectKind().GroupVersionKind().Kind, repo.ClusterDir).
		BuildWithResources(resource)
}

// ShouldBeInNamespacesError reports that an object belongs in namespaces/.
func ShouldBeInNamespacesError(resource client.Object) status.Error {
	return incorrectTopLevelDirectoryErrorBuilder.
		Sprintf("Namespace-scoped and Namespace configs MUST be declared in `%s/`. "+
			"To fix, move the %s to `%s/`.", repo.NamespacesDir, resource.GetObjectKind().GroupVersionKind().Kind, repo.NamespacesDir).
		BuildWithResources(resource)
}
