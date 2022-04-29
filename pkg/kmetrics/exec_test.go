/*
Copyright 2021 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kmetrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunKustomizeBuild(t *testing.T) {
	testCases := map[string]struct {
		inputDir    string
		flags       []string
		expected    string
		expectedErr string
	}{
		"simple": {
			inputDir: "./testdata/simple",
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    config.kubernetes.io/origin: |
      path: deployment.yaml
  name: my-deployment
spec:
  template:
    spec:
      containers:
      - image: my-image
        name: my-container-name
`,
		},
		"missing kustomization": {
			inputDir:    "./testdata/missingkustomization",
			expectedErr: "Error: unable to find one of 'kustomization.yaml', 'kustomization.yml' or 'Kustomization' in directory",
		},
		"complex": {
			inputDir:    "./testdata/complex",
			expectedErr: "base/deployment.yaml: no such file or directory",
		},
		"missing kustomization in base": {
			inputDir:    "./testdata/missingkustomizationinbase",
			expectedErr: "unable to find one of 'kustomization.yaml', 'kustomization.yml' or 'Kustomization' in directory",
		},
		"invalid kustomization": {
			inputDir:    "./testdata/invalidkustomization",
			expectedErr: "Error: json: cannot unmarshal string into Go struct field Kustomization.resources of type []string",
		},
		"multiple kustomization files": {
			inputDir:    "./testdata/multiplekustomizationfiles",
			expectedErr: "Error: Found multiple kustomization files",
		},
		"with generator": {
			inputDir: "./testdata/withgenerator",
			expected: `apiVersion: v1
data:
  foo: bar
kind: ConfigMap
metadata:
  annotations:
    config.kubernetes.io/origin: |
      configuredIn: kustomization.yaml
      configuredBy:
        apiVersion: builtin
        kind: ConfigMapGenerator
  name: my-config-798k5k7g9f
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    config.kubernetes.io/origin: |
      path: service.yaml
  name: my-service
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 9376
  selector:
    app: MyApp
---
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    config.kubernetes.io/origin: |
      path: deployment.yaml
  labels:
    app: nginx
  name: nginx-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
        ports:
        - containerPort: 80
`,
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			out, err := RunKustomizeBuild(context.Background(), false, tc.inputDir, tc.flags...)
			if tc.expectedErr == "" {
				if !assert.NoError(t, err) {
					t.FailNow()
				}
				if !assert.Equal(t, tc.expected, out) {
					t.FailNow()
				}
			} else {
				if !assert.Error(t, err) {
					t.FailNow()
				}
				if !assert.Contains(t, err.Error(), tc.expectedErr) {
					t.FailNow()
				}
			}
		})
	}
}
