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

package validate

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SelfReconcile checks if the given RootSync or RepoSync is the one that
// configures the reconciler.
func SelfReconcile(reconcilerName string) fileobjects.ObjectVisitor {
	return func(obj ast.FileObject) status.Error {
		switch obj.GetObjectKind().GroupVersionKind().GroupKind() {
		case kinds.RootSyncV1Beta1().GroupKind():
			if core.RootReconcilerName(obj.GetName()) == reconcilerName {
				return SelfReconcileError(obj)
			}
		case kinds.RepoSyncV1Beta1().GroupKind():
			if core.NsReconcilerName(obj.GetNamespace(), obj.GetName()) == reconcilerName {
				return SelfReconcileError(obj)
			}
		}
		return nil
	}
}

// SelfReconcileErrorCode is the code for an RootSync/RepoSync that reconciles itself.
var SelfReconcileErrorCode = "1069"

var selfReconcileErrorBuilder = status.NewErrorBuilder(SelfReconcileErrorCode)

// SelfReconcileError reports that a RootSync/RepoSync reconciles itself.
func SelfReconcileError(o client.Object) status.Error {
	kind := o.GetObjectKind().GroupVersionKind().Kind
	return selfReconcileErrorBuilder.
		Sprintf("%s %s/%s must not manage itself in its repo", kind, o.GetNamespace(), o.GetName()).
		BuildWithResources(o)
}
