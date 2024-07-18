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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
)

var (
	devLabelSelector     = labels.SelectorFromSet(map[string]string{"environment": "dev"})
	featureLabelSelector = labels.SelectorFromSet(map[string]string{"feature": "abc"})
)

func TestMatchChanged(t *testing.T) {
	testCases := []struct {
		name               string
		nsSelectors        map[string]labels.Selector
		selectedNamespaces map[string][]labels.Selector
		ns                 *corev1.Namespace
		wantChanged        bool
	}{
		{
			name:               "a Namespace was previously selected by two selectors, but not selected at all",
			nsSelectors:        map[string]labels.Selector{"dev-nss": devLabelSelector, "feature-nss": featureLabelSelector},
			selectedNamespaces: map[string][]labels.Selector{"test-ns": {devLabelSelector, featureLabelSelector}},
			ns:                 k8sobjects.NamespaceObject("test-ns", core.Label("environment", "prod")),
			wantChanged:        true,
		},
		{
			name:               "a Namespace was previously selected by two selectors, but only partially selected",
			nsSelectors:        map[string]labels.Selector{"dev-nss": devLabelSelector, "feature-nss": featureLabelSelector},
			selectedNamespaces: map[string][]labels.Selector{"test-ns": {devLabelSelector, featureLabelSelector}},
			ns:                 k8sobjects.NamespaceObject("test-ns", core.Label("environment", "dev")),
			wantChanged:        true,
		},
		{
			name:               "a Namespace was previously selected, and is also selected now",
			nsSelectors:        map[string]labels.Selector{"dev-nss": devLabelSelector, "feature-nss": featureLabelSelector},
			selectedNamespaces: map[string][]labels.Selector{"test-ns": {devLabelSelector, featureLabelSelector}},
			ns:                 k8sobjects.NamespaceObject("test-ns", core.Labels(map[string]string{"environment": "dev", "feature": "abc"})),
			wantChanged:        false,
		},
		{
			name:               "a Namespace was not selected before, but is selected now",
			nsSelectors:        map[string]labels.Selector{"test-nss": devLabelSelector},
			selectedNamespaces: map[string][]labels.Selector{"other-ns": {devLabelSelector}},
			ns:                 k8sobjects.NamespaceObject("test-ns", core.Label("environment", "dev")),
			wantChanged:        true,
		},
		{
			name:               "a Namespace is neither selected before nor selected now",
			nsSelectors:        map[string]labels.Selector{"test-nss": devLabelSelector},
			selectedNamespaces: map[string][]labels.Selector{"test-other": {devLabelSelector}},
			ns:                 k8sobjects.NamespaceObject("test-ns", core.Label("environment", "prod")),
			wantChanged:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := NewState()
			state.SetSelectorCache(tc.nsSelectors, tc.selectedNamespaces)
			changed := state.matchChanged(tc.ns)
			assert.Equal(t, tc.wantChanged, changed)
		})
	}
}
