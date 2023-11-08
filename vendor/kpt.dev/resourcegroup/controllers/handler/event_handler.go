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

package handler

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/resourcemap"
)

// resourceMap provides the interface for access the cached resources.
type resourceMap interface {
	// Get maps from GKNN -> []RG. It gets the identifiers of ResourceGroup CRs
	// that a GKNN is in.
	Get(gknn v1alpha1.ObjMetadata) []types.NamespacedName
	// GetResources map from GK -> []GKNN. It gets the list of GKNN for
	// a given group kind.
	GetResources(gk schema.GroupKind) []v1alpha1.ObjMetadata
	// SetStatus sets the cached status for the given resource.
	SetStatus(res v1alpha1.ObjMetadata, resStatus *resourcemap.CachedStatus)
}

// EnqueueEventToChannel pushes an event to ResourceGroup event channel
// instead of enqueue a Reqeust for ResourceGroup.
type EnqueueEventToChannel struct {
	Mapping resourceMap
	Channel chan event.GenericEvent
	Log     logr.Logger
	GVK     schema.GroupVersionKind
}

var _ cache.ResourceEventHandler = &EnqueueEventToChannel{}

// OnAdd implements EventHandler
func (e *EnqueueEventToChannel) OnAdd(obj interface{}) {
	e.Log.V(5).Info("received an add event")
	e.enqueueEvent(obj)
}

// OnUpdate implements EventHandler
func (e *EnqueueEventToChannel) OnUpdate(_, newObj interface{}) {
	e.Log.V(5).Info("received an update event")
	e.enqueueEvent(newObj)
}

// OnDelete implements EventHandler
func (e *EnqueueEventToChannel) OnDelete(obj interface{}) {
	e.Log.V(5).Info("received a delete event")
	e.enqueueEvent(obj)
}

func (e *EnqueueEventToChannel) enqueueEvent(obj interface{}) {
	gknn, err := e.toGKNN(obj)
	if err != nil {
		e.Log.Error(err, "failed to get GKNN from the received event", "object", obj)
		return
	}
	for _, r := range e.Mapping.Get(gknn) {
		var resgroup = &v1alpha1.ResourceGroup{}
		resgroup.SetNamespace(r.Namespace)
		resgroup.SetName(r.Name)
		e.Log.V(5).Info("send a generic event for", "resourcegroup", resgroup.GetObjectMeta())
		e.Channel <- event.GenericEvent{Object: resgroup}
	}
}

func (e EnqueueEventToChannel) toGKNN(obj interface{}) (v1alpha1.ObjMetadata, error) {
	metadata, err := meta.Accessor(obj)
	if err != nil {
		e.Log.Error(err, "missing object meta")
		return v1alpha1.ObjMetadata{}, fmt.Errorf("missing object meta: %v", err)
	}
	gknn := v1alpha1.ObjMetadata{
		Namespace: metadata.GetNamespace(),
		Name:      metadata.GetName(),
		GroupKind: v1alpha1.GroupKind{
			Group: e.GVK.Group,
			Kind:  e.GVK.Kind,
		},
	}
	return gknn, nil
}
