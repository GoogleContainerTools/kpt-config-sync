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
	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// CRDEventHandler pushes an event to ResourceGroup event channel
// when the CRD or its CRs are contained in some ResourceGroup CRs.
type CRDEventHandler struct {
	Mapping resourceMap
	Channel chan event.GenericEvent
	Log     logr.Logger
}

var _ handler.EventHandler = &CRDEventHandler{}

// Create implements EventHandler
func (h *CRDEventHandler) Create(e event.CreateEvent, _ workqueue.RateLimitingInterface) {
	h.Log.V(5).Info("received a create event")
	h.enqueueEvent(e.Object)
}

// Update implements EventHandler
func (h *CRDEventHandler) Update(e event.UpdateEvent, _ workqueue.RateLimitingInterface) {
	h.Log.V(5).Info("received an update event")
	h.enqueueEvent(e.ObjectNew)
}

// Delete implements EventHandler
func (h *CRDEventHandler) Delete(e event.DeleteEvent, _ workqueue.RateLimitingInterface) {
	h.Log.V(5).Info("received a delete event")
	h.enqueueEvent(e.Object)
}

// Generic implements EventHandler
func (h *CRDEventHandler) Generic(e event.GenericEvent, _ workqueue.RateLimitingInterface) {
	h.Log.V(5).Info("received a generic event")
	h.enqueueEvent(e.Object)
}

func (h *CRDEventHandler) enqueueEvent(obj client.Object) {
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		h.Log.Info("failed to derive a CRD from the event object", "name", obj.GetName())
		return
	}
	gk := schema.GroupKind{
		Group: crd.Spec.Group,
		Kind:  crd.Spec.Names.Kind,
	}
	for _, gknn := range h.Mapping.GetResources(gk) {
		// clear the cached status for gknn since the CR status could change
		// due to the change of CRD.
		h.Log.V(5).Info("reset the cached resource status", "resource", gknn)
		h.Mapping.SetStatus(gknn, nil)
		for _, r := range h.Mapping.Get(gknn) {
			var resgroup = &v1alpha1.ResourceGroup{}
			resgroup.SetNamespace(r.Namespace)
			resgroup.SetName(r.Name)
			h.Log.V(5).Info("send a generic event for", "resourcegroup", resgroup.GetObjectMeta())
			h.Channel <- event.GenericEvent{Object: resgroup}
		}
	}
}
