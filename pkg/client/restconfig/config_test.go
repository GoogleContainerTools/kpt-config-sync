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

package restconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestFilterContexts(t *testing.T) {
	testCases := map[string]struct {
		wantContexts     []string
		contexts         map[string]*clientcmdapi.Context
		expectedContexts map[string]*clientcmdapi.Context
	}{
		"wantContexts is nil": {
			contexts: map[string]*clientcmdapi.Context{
				"context-1": nil,
				"context-2": nil,
			},
			expectedContexts: map[string]*clientcmdapi.Context{
				"context-1": nil,
				"context-2": nil,
			},
		},
		"wantContexts selects an element": {
			wantContexts: []string{"context-1"},
			contexts: map[string]*clientcmdapi.Context{
				"context-1": nil,
				"context-2": nil,
			},
			expectedContexts: map[string]*clientcmdapi.Context{
				"context-1": nil,
			},
		},
		"wantContexts is set but matches no elements": {
			wantContexts: []string{},
			contexts: map[string]*clientcmdapi.Context{
				"context-1": nil,
				"context-2": nil,
			},
			expectedContexts: map[string]*clientcmdapi.Context{},
		},
		"contexts is empty": {
			wantContexts:     []string{"cluster-1"},
			contexts:         map[string]*clientcmdapi.Context{},
			expectedContexts: map[string]*clientcmdapi.Context{},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			gotContexts := filterContexts(tc.wantContexts, tc.contexts)
			assert.Equal(t, tc.expectedContexts, gotContexts)
		})
	}
}
