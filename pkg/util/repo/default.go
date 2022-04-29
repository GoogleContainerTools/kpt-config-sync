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

package repo

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
)

// CurrentVersion is the version of the format for the ConfigManagement Repo.
const CurrentVersion = "1.0.0"

// Default returns a default Repo in case one is not defined in the source of truth.
func Default() *v1.Repo {
	return setTypeMeta(&v1.Repo{
		ObjectMeta: metav1.ObjectMeta{
			Name: "repo",
		},
		Spec: v1.RepoSpec{
			Version: CurrentVersion,
		},
	})
}

// setTypeMeta sets the fields for TypeMeta since they are usually unset when fetching a Repo from
// kubebuilder cache for some reason.
func setTypeMeta(r *v1.Repo) *v1.Repo {
	r.TypeMeta = metav1.TypeMeta{
		Kind:       kinds.Repo().Kind,
		APIVersion: kinds.Repo().GroupVersion().String(),
	}
	return r
}
