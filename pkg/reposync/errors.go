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

package reposync

import (
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// Errors returns the errors referred by `errorSources`.
func Errors(rs *v1beta1.RepoSync, errorSources []v1beta1.ErrorSource) []v1beta1.ConfigSyncError {
	var errs []v1beta1.ConfigSyncError
	if rs == nil {
		return errs
	}

	for _, errorSource := range errorSources {
		switch errorSource {
		case v1beta1.RenderingError:
			errs = append(errs, rs.Status.Rendering.Errors...)
		case v1beta1.SourceError:
			errs = append(errs, rs.Status.Source.Errors...)
		case v1beta1.SyncError:
			errs = append(errs, rs.Status.Sync.Errors...)
		}
	}
	return errs
}
