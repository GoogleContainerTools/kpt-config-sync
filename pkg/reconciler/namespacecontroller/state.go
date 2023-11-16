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

package namespacecontroller

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// State stores the state of the Namespace Controller.
type State struct {
	// eventMux is a RMMutex to protect read/write to 'isSyncingPending'.
	// It is RWMutex because the cache is read more often than written.
	eventMux sync.RWMutex

	// isSyncPending indicates whether the namespace controller fires up an event that requires a new sync.
	isSyncPending bool

	// selectorMux is the mutex to protect read/write to 'nsSelectors' and 'selectedNamespaces'.
	selectorMux sync.Mutex

	// nsSelectors is a map of NamespaceSelector names to the LabelSelectors.
	nsSelectors map[string]labels.Selector

	// selectedNamespaces records the dynamic/on-cluster Namespaces selected by NamespaceSelectors.
	// It is a map from the Namespace names to the list of NamespaceSelector's LabelSelectors.
	// A Namespace can be selected by multiple NamespaceSelectors.
	// The LabelSelectors determine whether a Namespace's select state is changed.
	selectedNamespaces map[string][]labels.Selector
}

// NewState instantiates the namespace controller state.
func NewState() *State {
	return &State{
		nsSelectors:        make(map[string]labels.Selector),
		selectedNamespaces: make(map[string][]labels.Selector),
	}
}

// setSyncPending indicates an event is detected to trigger a new sync.
func (s *State) setSyncPending() {
	s.eventMux.Lock()
	defer s.eventMux.Unlock()
	s.isSyncPending = true
}

// ScheduleSync checks whether the isSyncPending flag is true.
// If it is true, a sync should be scheduled by the reconciler thread, and it
// flips the internal flag to false.
// If it is false, no sync will be scheduled.
func (s *State) ScheduleSync() bool {
	s.eventMux.Lock()
	defer s.eventMux.Unlock()
	isSyncingPending := s.isSyncPending
	if s.isSyncPending {
		// Reset the Sync pending state after a sync regardless if succeeds or not.
		// If it fails, a retry should be triggered.
		s.isSyncPending = false
	}
	return isSyncingPending
}

// SetSelectorCache is invoked by the reconciler to set the cache after parsing the NamespaceSelectors.
func (s *State) SetSelectorCache(nsSelectors map[string]labels.Selector, selectedNamespaces map[string][]labels.Selector) {
	s.selectorMux.Lock()
	defer s.selectorMux.Unlock()

	nssMapCopy := make(map[string]labels.Selector, len(nsSelectors))
	selectedNSMapCopy := make(map[string][]labels.Selector, len(selectedNamespaces))
	for nss, selector := range nsSelectors {
		nssMapCopy[nss] = selector
	}
	for ns, selector := range selectedNamespaces {
		selectedNSMapCopy[ns] = selector
	}

	s.nsSelectors = nssMapCopy
	s.selectedNamespaces = selectedNSMapCopy
}

// isSelectedNamespace returns whether the namespace is previously selected.
func (s *State) isSelectedNamespace(ns string) bool {
	s.selectorMux.Lock()
	defer s.selectorMux.Unlock()

	_, found := s.selectedNamespaces[ns]
	return found
}

// matchChanged returns whether the match of Namespace's labels and the cached selector has changed.
func (s *State) matchChanged(ns *corev1.Namespace) bool {
	s.selectorMux.Lock()
	defer s.selectorMux.Unlock()

	nsSelectors, found := s.selectedNamespaces[ns.Name]
	if !found {
		for _, selector := range s.nsSelectors {
			// The Namespace is not selected before, but is now selected.
			if selector.Matches(labels.Set(ns.GetLabels())) {
				return true
			}
		}
		// The Namespace is neither selected before, nor selected now, so no change.
		return false
	}

	for _, selector := range nsSelectors {
		// The Namespace is previously selected, but no longer selected by the selector.
		if !selector.Matches(labels.Set(ns.GetLabels())) {
			return true
		}
	}
	// The Namespace is selected both before and now.
	return false
}
