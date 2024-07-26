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

package kinds

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// DeploymentResource returns the canonical Deployment GroupVersionResource.
func DeploymentResource() schema.GroupVersionResource {
	return appsv1.SchemeGroupVersion.WithResource("deployments")
}

// RootSyncResource returns the canonical RootSync GroupVersionResource.
func RootSyncResource() schema.GroupVersionResource {
	return configsyncv1beta1.SchemeGroupVersion.WithResource("rootsyncs")
}

// RepoSyncResource returns the canonical RepoSync GroupVersionResource.
func RepoSyncResource() schema.GroupVersionResource {
	return configsyncv1beta1.SchemeGroupVersion.WithResource("reposyncs")
}

// RootSyncRESTMapping returns the canonical RootSync RESTMapping.
func RootSyncRESTMapping() *meta.RESTMapping {
	return &meta.RESTMapping{
		Resource:         RootSyncResource(),
		GroupVersionKind: RootSyncV1Beta1(),
		Scope:            meta.RESTScopeNamespace,
	}
}

// RepoSyncRESTMapping returns the canonical RepoSync RESTMapping.
func RepoSyncRESTMapping() *meta.RESTMapping {
	return &meta.RESTMapping{
		Resource:         RepoSyncResource(),
		GroupVersionKind: RepoSyncV1Beta1(),
		Scope:            meta.RESTScopeNamespace,
	}
}
