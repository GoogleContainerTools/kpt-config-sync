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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// cacheForCommit tracks the progress made by the reconciler for a source commit (a source commit or an oci image digest).
//
// The reconciler resets the whole cache when a new commit is detected.
//
// The reconciler resets the whole cache except for the cached sourceState when:
//   - a force-resync happens, or
//   - one of the watchers noticed a management conflict.
type cacheForCommit struct {
	// source tracks the state of the source repo.
	// This field is only set after the reconciler successfully reads all the source files.
	source *sourceState

	// parse tracks the state of the parse stage.
	parse *parseResult

	// declaredResourcesUpdated indicates whether the resource declaration set
	// has been updated.
	declaredResourcesUpdated bool

	// applied indicates whether the applier has successfully applied the
	// declared resources.
	applied bool

	// watchesUpdated indicates whether the remediator watches have been updated
	// for the latest declared resources.
	watchesUpdated bool

	// needToRetry indicates whether a retry is needed.
	needToRetry bool
}

// UpdateParseResult updates the object cache with the results from parsing from the
// file cache.
func (c *cacheForCommit) UpdateParseResult(objs []ast.FileObject, parserErrs status.MultiError, now metav1.Time) {
	knownScopeObjs, unknownScopeObjs := splitObjects(objs)
	c.parse = &parseResult{
		objsSkipped:    unknownScopeObjs,
		objsToApply:    knownScopeObjs,
		parserErrs:     parserErrs,
		lastUpdateTime: now,
	}
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

type parseResult struct {
	// objsSkipped contains the objects which will not be sent to the applier to apply.
	// For example, the objects whose scope is unknown will not be sent to the applier since
	// the kpt applier cannot handle unknown-scoped objects.
	objsSkipped []ast.FileObject

	// objsToApply contains the objects which will be sent to the applier to apply.
	objsToApply []ast.FileObject

	// parserErrs includes the parser errors.
	parserErrs status.MultiError

	// lastUpdateTime is the last time the parse result was updated.
	lastUpdateTime metav1.Time
}

// IsUpdateRequired returns true if the parse result has not been updated since
// the last reset, or there were errors, or there were skipped objects (due to
// unknown scope).
func (p *parseResult) IsUpdateRequired() bool {
	return p == nil || p.lastUpdateTime.IsZero() || p.parserErrs != nil || len(p.objsSkipped) > 0
}

// GKVs returns the set of unique GVKs for all the parsed objects.
func (p *parseResult) GKVs() []schema.GroupVersionKind {
	seen := make(map[schema.GroupVersionKind]bool)
	var gvks []schema.GroupVersionKind
	for _, o := range p.fileObjects() {
		gvk := o.GetObjectKind().GroupVersionKind()
		if !seen[gvk] {
			seen[gvk] = true
			gvks = append(gvks, gvk)
		}
	}
	// The order of GVKs is not deterministic, but we're using it for
	// toWebhookConfiguration which does not require its input to be sorted.
	return gvks
}

// fileObjects returns all the parsed objects.
func (p *parseResult) fileObjects() []ast.FileObject {
	objs := make([]ast.FileObject, 0, len(p.objsToApply)+len(p.objsSkipped))
	objs = append(objs, p.objsToApply...)
	objs = append(objs, p.objsSkipped...)
	return objs
}
