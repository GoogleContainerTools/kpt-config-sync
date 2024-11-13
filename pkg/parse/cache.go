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

package parse

import (
	"sort"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncclient"
)

// CacheForCommit tracks the progress made by the reconciler for a source commit (a source commit or an oci image digest).
//
// The reconciler resets the whole cache when a new commit is detected.
//
// The reconciler resets the whole cache except for the cached SourceState when:
//   - a force-resync happens, or
//   - one of the watchers noticed a management conflict.
type CacheForCommit struct {
	// Source tracks the state of the Source repo.
	// This field is only set after the reconciler successfully reads all the Source files.
	Source *syncclient.SourceState

	// HasParserResult indicates whether the cache includes the parser result.
	HasParserResult bool

	// ObjsSkipped contains the objects which will not be sent to the applier to apply.
	// For example, the objects whose scope is unknown will not be sent to the applier since
	// the kpt applier cannot handle unknown-scoped objects.
	ObjsSkipped []ast.FileObject

	// ObjsToApply contains the objects which will be sent to the applier to apply.
	ObjsToApply []ast.FileObject

	// ParserErrs includes the parser errors.
	ParserErrs status.MultiError

	// DeclaredResourcesUpdated indicates whether the resource declaration set
	// has been updated.
	DeclaredResourcesUpdated bool

	// Applied indicates whether the applier has successfully Applied the
	// declared resources.
	Applied bool

	// WatchesUpdated indicates whether the remediator watches have been updated
	// for the latest declared resources.
	WatchesUpdated bool

	// NeedToRetry indicates whether a retry is needed.
	NeedToRetry bool
}

func (c *CacheForCommit) SetParserResult(objs []ast.FileObject, parserErrs status.MultiError) {
	knownScopeObjs, unknownScopeObjs := splitObjects(objs)
	c.ObjsSkipped = unknownScopeObjs
	c.ObjsToApply = knownScopeObjs
	c.ParserErrs = parserErrs
	c.HasParserResult = true
}

func (c *CacheForCommit) ParserResultUpToDate() bool {
	// If len(c.objsSkipped) > 0, it mean that some objects were skipped to be sent to
	// the kpt applier. For example, the objects whose scope is unknown will not be sent
	// to the applier since the kpt applier cannot handle unknown-scoped objects.
	// Therefore, if len(c.objsSkipped) > 0, we would parse the configs from scratch.
	return c.HasParserResult && len(c.ObjsSkipped) == 0 && c.ParserErrs == nil
}

// splitObjects splits `objs` into two groups: the objects whose scope is known, and the objects whose scope is unknown.
func splitObjects(objs []ast.FileObject) ([]ast.FileObject, []ast.FileObject) {
	var knownScopeObjs, unknownScopeObjs []ast.FileObject
	var unknownScopeIDs []string
	for _, obj := range objs {
		if core.GetAnnotation(obj, metadata.UnknownScopeAnnotationKey) == metadata.UnknownScopeAnnotationValue {
			unknownScopeObjs = append(unknownScopeObjs, obj)
			unknownScopeIDs = append(unknownScopeIDs, core.GKNN(obj.Unstructured))
		} else {
			knownScopeObjs = append(knownScopeObjs, obj)
		}
	}
	if len(unknownScopeIDs) > 0 {
		sort.Strings(unknownScopeIDs)
		klog.Infof("Skip sending %v unknown-scoped objects to the applier: %v", len(unknownScopeIDs), unknownScopeIDs)
	}
	return knownScopeObjs, unknownScopeObjs
}
