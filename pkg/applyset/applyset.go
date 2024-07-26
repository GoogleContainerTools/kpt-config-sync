// Copyright 2024 Google LLC
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

package applyset

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	kubectlapply "k8s.io/kubectl/pkg/cmd/apply"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
)

// IDFromSync generates an ApplySet ID for the RootSync or RepoSync as
// an ApplySet parent.
func IDFromSync(syncName string, syncScope declared.Scope) string {
	return FromSync(syncName, syncScope, nil, nil).ID()
}

// FromSync constructs a new ApplySet for the specified RootSync or RepoSync.
// The RESTMapper & RESTClient are optional, depending on which methods you plan
// to call.
func FromSync(syncName string, syncScope declared.Scope, mapper meta.RESTMapper, client resource.RESTClient) *kubectlapply.ApplySet {
	tooling := kubectlapply.ApplySetTooling{
		Name:    metadata.ApplySetToolingName,
		Version: metadata.ApplySetToolingVersion,
	}
	parent := &kubectlapply.ApplySetParentRef{
		Name:      syncName,
		Namespace: syncScope.SyncNamespace(),
	}
	switch syncScope {
	case declared.RootScope:
		parent.RESTMapping = kinds.RootSyncRESTMapping()
	default:
		parent.RESTMapping = kinds.RepoSyncRESTMapping()
	}
	return kubectlapply.NewApplySet(parent, tooling, mapper, client)
}

// ParseTooling parses the tooling value with format NAME/VERSION.
// Returns an error if the input does not contain at least one slash (`/`).
func ParseTooling(toolingValue string) (kubectlapply.ApplySetTooling, error) {
	parts := strings.Split(toolingValue, "/")
	if len(parts) >= 2 {
		return kubectlapply.ApplySetTooling{
			Name:    strings.Join(parts[:len(parts)-1], "/"),
			Version: parts[len(parts)-1],
		}, nil
	}
	// Invalid
	return kubectlapply.ApplySetTooling{},
		fmt.Errorf("invalid applyset tooling value: expected NAME/VERSION: %s", toolingValue)
}

// FormatTooling returns a formatted value for the ApplySet tooling annotation.
func FormatTooling(name, version string) string {
	tooling := kubectlapply.ApplySetTooling{
		Name:    name,
		Version: version,
	}
	return tooling.String()
}
