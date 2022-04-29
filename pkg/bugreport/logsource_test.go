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

package bugreport

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestLogSourceGetPathName(t *testing.T) {
	tests := []struct {
		name         string
		source       logSource
		expectedName string
	}{
		{
			name: "valid loggable produces hyphenated name",
			source: logSource{
				ns:   *fake.NamespaceObject("myNamespace"),
				pod:  *fake.PodObject("myPod", make([]v1.Container, 0)),
				cont: *fake.ContainerObject("myContainer"),
			},
			expectedName: "namespaces/myNamespace/myPod/myContainer",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			actualName := test.source.pathName()
			if actualName != test.expectedName {
				t.Errorf("Expected loggable name to be %v, but received %v", test.expectedName, actualName)
			}
		})
	}
}
