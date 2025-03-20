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

package declared

import (
	"context"
	"sync"

	"github.com/elliotchance/orderedmap/v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resources is a threadsafe container for a set of resources declared in a Git
// repo.
type Resources struct {
	mutex sync.RWMutex
	// declaredObjectsMap is a map of object IDs to the unstructured format of those
	// objects. Note that the pointer to this map is threadsafe but the map itself
	// is not threadsafe. This map should never be returned from a function
	// directly. The map should never be written to once it has been assigned to
	// this reference; it should be treated as read-only from then on.
	declaredObjectsMap *orderedmap.OrderedMap[core.ID, *unstructured.Unstructured]

	// mutationIgnoredObjectsMap is a map of object IDs to the cluster-state of mutation-ignored objects.
	// The cluster-state is initialized by the applier and updated by the remediator.
	mutationIgnoredObjectsMap *orderedmap.OrderedMap[core.ID, client.Object]

	// commit of the source in which the resources were declared
	commit string
	// previousCommit is the preceding commit to the commit
	previousCommit string
}

// UpdateIgnored performs an atomic update on the resource ignore mutation set.
func (r *Resources) UpdateIgnored(objs ...client.Object) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.mutationIgnoredObjectsMap == nil {
		r.mutationIgnoredObjectsMap = orderedmap.NewOrderedMap[core.ID, client.Object]()
	}

	for _, o := range objs {
		if _, wasDeleted := o.(*queue.Deleted); wasDeleted {
			r.mutationIgnoredObjectsMap.Set(core.IDOf(o), o)
		} else {
			u, _ := reconcile.AsUnstructuredSanitized(o)
			r.mutationIgnoredObjectsMap.Set(core.IDOf(u), u)
		}
	}

}

// GetIgnored returns a copy of a declared object that has the ignore mutation annotation
func (r *Resources) GetIgnored(id core.ID) (client.Object, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.mutationIgnoredObjectsMap == nil || r.mutationIgnoredObjectsMap.Len() == 0 {
		return nil, false
	}

	o, found := r.mutationIgnoredObjectsMap.Get(id)

	if found {
		oCopy := o.DeepCopyObject().(client.Object)
		return oCopy, found
	}

	return o, found
}

// IgnoredObjects returns a slice with a copy of all ignore-mutation objects in the ignoredObjsMap
func (r *Resources) IgnoredObjects() []client.Object {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.mutationIgnoredObjectsMap == nil || r.mutationIgnoredObjectsMap.Len() == 0 {
		return nil
	}

	var objects []client.Object
	for pair := r.mutationIgnoredObjectsMap.Front(); pair != nil; pair = pair.Next() {
		objects = append(objects, pair.Value.DeepCopyObject().(client.Object))
	}
	return objects
}

// DeleteIgnored deletes an ignore-mutation object from the ignored cache
func (r *Resources) DeleteIgnored(id core.ID) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.mutationIgnoredObjectsMap == nil || r.mutationIgnoredObjectsMap.Len() == 0 {
		return false
	}

	return r.mutationIgnoredObjectsMap.Delete(id)
}

// UpdateDeclared performs an atomic update on the resource declaration set.
func (r *Resources) UpdateDeclared(ctx context.Context, objects []client.Object, commit string) ([]client.Object, status.Error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	// First build up the new map using a local pointer/reference.
	newSet := orderedmap.NewOrderedMap[core.ID, *unstructured.Unstructured]()
	newObjects := []client.Object{}
	for _, obj := range objects {
		if obj == nil {
			klog.Warning("Resources received nil declared resource")
			metrics.RecordInternalError(ctx, "parser")
			continue
		}
		id := core.IDOf(obj)
		u, err := reconcile.AsUnstructuredSanitized(obj)
		if err != nil {
			// This should never happen.
			return nil, status.InternalErrorBuilder.Wrap(err).
				Sprintf("converting %v to unstructured.Unstructured", id).Build()
		}
		newSet.Set(id, u)
		newObjects = append(newObjects, obj)
	}

	// Record the declared_resources metric, after parsing but before validation.
	metrics.RecordDeclaredResources(ctx, commit, len(newObjects))
	if r.previousCommit != commit && r.previousCommit != "" {
		// For Cloud Monitoring, we have configured otel-collector to remove the
		// commit label, to reduce cardinality, but this aggregation uses the max
		// value (b/321875474). So in order for the latest commit to be chosen as
		// the max value, we reset the previous commit value to zero.
		// TODO: Remove this workaround after migrating to the otel-collector metrics client and switching from gauge to async gauge
		metrics.RecordDeclaredResources(ctx, r.previousCommit, 0)
	}

	if err := deletesAllNamespaces(r.declaredObjectsMap, newSet); err != nil {
		return nil, err
	}

	r.previousCommit = commit
	r.declaredObjectsMap = newSet
	r.commit = commit
	return newObjects, nil
}

// GetDeclared returns a copy of the resource declaration as read from Git
func (r *Resources) GetDeclared(id core.ID) (*unstructured.Unstructured, string, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.declaredObjectsMap == nil || r.declaredObjectsMap.Len() == 0 {
		return nil, r.commit, false
	}
	u, found := r.declaredObjectsMap.Get(id)
	// We return a copy of the Unstructured, as
	// 1) client.Client methods mutate the objects passed into them.
	// 2) We don't want to persist any changes made to an object we retrieved
	//  from a declared.Resources.
	return u.DeepCopy(), r.commit, found
}

// DeclaredUnstructureds returns all resource objects declared in the source,
// along with the source commit.
func (r *Resources) DeclaredUnstructureds() []*unstructured.Unstructured {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.declaredObjectsMap == nil || r.declaredObjectsMap.Len() == 0 {
		return nil
	}
	var objects []*unstructured.Unstructured
	for pair := r.declaredObjectsMap.Front(); pair != nil; pair = pair.Next() {
		objects = append(objects, pair.Value)
	}
	return objects
}

// DeclaredObjects returns all resource objects declared in the source
func (r *Resources) DeclaredObjects() []client.Object {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.declaredObjectsMap == nil || r.declaredObjectsMap.Len() == 0 {
		return nil
	}
	var objects []client.Object
	for pair := r.declaredObjectsMap.Front(); pair != nil; pair = pair.Next() {
		objects = append(objects, pair.Value)
	}
	return objects
}

// DeclaredGVKs returns the set of all GroupVersionKind found in the source,
// along with the source commit.
func (r *Resources) DeclaredGVKs() (map[schema.GroupVersionKind]struct{}, string) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.declaredObjectsMap == nil || r.declaredObjectsMap.Len() == 0 {
		return nil, r.commit
	}
	gvkSet := make(map[schema.GroupVersionKind]struct{})
	for pair := r.declaredObjectsMap.Front(); pair != nil; pair = pair.Next() {
		gvkSet[pair.Value.GroupVersionKind()] = struct{}{}
	}
	return gvkSet, r.commit
}

// DeclaredCRDs returns the list of CRDs declared in the source.
func (r *Resources) DeclaredCRDs(scheme *runtime.Scheme) ([]*apiextensionsv1.CustomResourceDefinition, status.MultiError) {
	// DeclaredUnstructureds handles the mutex, so this method doesn't need to lock.
	var crds []*apiextensionsv1.CustomResourceDefinition
	declaredObjs := r.DeclaredUnstructureds()
	for _, obj := range declaredObjs {
		if obj.GroupVersionKind().GroupKind() != kinds.CustomResourceDefinition() {
			continue
		}
		crd, err := clusterconfig.ToCRD(obj, scheme)
		if err != nil {
			return nil, err
		}
		crds = append(crds, crd)
	}
	return crds, nil
}

// GetCommit returns the source commit connected with the declared resources.
func (r *Resources) GetCommit() string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.commit
}
