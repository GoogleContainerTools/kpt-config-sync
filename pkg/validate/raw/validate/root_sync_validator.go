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

package validate

import (
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/yaml"
)

// RootSync checks if the given FileObject is a RootSync and if so, verifies
// that its fields are valid.
func RootSync(obj ast.FileObject) status.Error {
	if obj.GetObjectKind().GroupVersionKind().GroupKind() != kinds.RootSyncV1Beta1().GroupKind() {
		return nil
	}
	s, err := obj.Structured()
	if err != nil {
		return err
	}
	var rs *v1beta1.RootSync
	if obj.GroupVersionKind() == kinds.RootSyncV1Alpha1() {
		rs, err = toRootSyncV1Beta1(s.(*v1alpha1.RootSync))
		if err != nil {
			return err
		}
	} else {
		rs = s.(*v1beta1.RootSync)
	}
	if rs.Spec.SourceType == "" {
		rs.Spec.SourceType = string(v1beta1.GitSource)
	}
	return SourceSpec(rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, rs)
}

func toRootSyncV1Beta1(rs *v1alpha1.RootSync) (*v1beta1.RootSync, status.Error) {
	data, err := yaml.Marshal(rs)
	if err != nil {
		return nil, status.ResourceWrap(err, "failed marshalling", rs)
	}
	s := &v1beta1.RootSync{}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, status.ResourceWrap(err, "failed to convert to v1beta1 RootSync", rs)
	}
	return s, nil
}
