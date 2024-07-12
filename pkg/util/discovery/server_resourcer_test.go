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

package discovery

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// Test mock of ServerResourcer
type CustomServerResourcer struct {
	ExpectedResources []*v1.APIResourceList
	ExpectedError     error
	Called            bool
}

func (m *CustomServerResourcer) ServerGroupsAndResources() ([]*v1.APIGroup, []*v1.APIResourceList, error) {
	m.Called = true
	return nil, m.ExpectedResources, m.ExpectedError
}

func (m *CustomServerResourcer) Setup(expectedResources []*v1.APIResourceList, expectedError error) {
	m.ExpectedResources = expectedResources
	m.ExpectedError = expectedError
}

func TestGetResources(t *testing.T) {
	testCases := []struct {
		name              string
		mockSetup         func(*CustomServerResourcer)
		expectedResources []*v1.APIResourceList
		expectedError     error
	}{
		{
			name: "Success - Resources Retrieved",
			mockSetup: func(m *CustomServerResourcer) {
				m.Setup([]*v1.APIResourceList{{}}, nil)
			},
			expectedResources: []*v1.APIResourceList{{}},
			expectedError:     nil,
		},
		{
			name: "Error - Discovery Error",
			mockSetup: func(m *CustomServerResourcer) {
				m.Setup(nil, errors.New("discovery failure"))
			},
			expectedResources: nil,
			expectedError:     errors.New("API discovery failed"),
		},
		{
			name: "Error - Empty Response Error Handled Gracefully",
			mockSetup: func(m *CustomServerResourcer) {
				discoErr := &discovery.ErrGroupDiscoveryFailed{
					Groups: map[schema.GroupVersion]error{{Group: "external.metrics.k8s.io", Version: "v1beta1"}: errors.New("received empty response")},
				}
				m.Setup([]*v1.APIResourceList{{}}, discoErr)
			},
			expectedResources: []*v1.APIResourceList{{}},
			expectedError:     nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockResourcer := new(CustomServerResourcer)
			tc.mockSetup(mockResourcer)

			resources, err := GetResources(mockResourcer)

			assert.Equal(t, tc.expectedResources, resources)
			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.True(t, mockResourcer.Called, "Expected ServerGroupsAndResources to be called but it wasn't")
		})
	}
}
