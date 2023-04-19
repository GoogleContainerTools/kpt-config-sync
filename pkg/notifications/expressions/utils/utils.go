// Copyright 2023 Google LLC
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

package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util"
)

// New returns the expression functions for "utils"
// this is a set of helper functions for RootSync/RepoSync fields
func New(obj *unstructured.Unstructured, apiKind string) map[string]interface{} {
	return map[string]interface{}{
		"LastCommit":       lastCommit(obj, apiKind),
		"ConfigSyncErrors": configSyncErrors(obj, apiKind),
	}
}

// return the commit from the Syncing condition
func lastCommit(obj *unstructured.Unstructured, apiKind string) func() string {
	cached := false
	commit := ""
	return func() string {
		if cached {
			return commit
		}
		cached = true
		var err error
		commit, err = util.LastCommit(obj, apiKind)
		if err != nil {
			klog.Errorf("failed to parse LastCommit: %v", err)
		}
		return commit
	}
}

// return an array of all ConfigSyncError from the various status objects
func configSyncErrors(obj *unstructured.Unstructured, apiKind string) func() []v1beta1.ConfigSyncError {
	var cached bool
	var errs []v1beta1.ConfigSyncError
	return func() []v1beta1.ConfigSyncError {
		if cached {
			return errs
		}
		cached = true
		var status *v1beta1.Status
		switch apiKind {
		case kinds.RootSyncV1Beta1().Kind:
			rs := &v1beta1.RootSync{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), rs); err != nil {
				klog.Errorf("failed to convert unstructured to RootSync: %v", err)
				return errs
			}
			status = &rs.Status.Status
		case kinds.RepoSyncV1Beta1().Kind:
			rs := &v1beta1.RepoSync{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), rs); err != nil {
				klog.Errorf("failed to convert unstructured to RepoSync: %v", err)
				return errs
			}
			status = &rs.Status.Status
		}
		if status == nil {
			klog.Errorf("failed to get Status from unstructured")
			return errs
		}
		errs = append(errs, status.Source.Errors...)
		errs = append(errs, status.Rendering.Errors...)
		errs = append(errs, status.Sync.Errors...)
		return errs
	}
}
