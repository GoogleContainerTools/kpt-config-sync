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

package hydrate

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/clientgen/apis/scheme"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/validate/objects"
)

func TestRawYAML(t *testing.T) {
	// We use literal YAML here instead of objects as:
	// 1) If we used literal structs the protocol field would implicitly be added.
	// 2) It's really annoying to specify these as Unstructureds.
	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "Pod",
			yaml: `
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: bookstore
spec:
  containers:
  - image: nginx:1.7.9
    name: nginx
    ports:
    - containerPort: 80
`,
		},
		{
			name: "Pod with protocol",
			yaml: `
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: bookstore
spec:
  containers:
  - image: nginx:1.7.9
    name: nginx
    ports:
    - containerPort: 80
      protocol: TCP
`,
		},
		{
			name: "Pod initContainers",
			yaml: `
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: bookstore
spec:
  initContainers:
  - image: nginx:1.7.9
    name: nginx
    ports:
    - containerPort: 80
`,
		},
		{
			name: "ReplicationController",
			yaml: `
apiVersion: v1
kind: ReplicationController
metadata:
  name: nginx
  namespace: bookstore
spec:
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "DaemonSet",
			yaml: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nginx
  namespace: bookstore
spec:
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "Deployment",
			yaml: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: bookstore
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
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "ReplicaSet",
			yaml: `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: nginx
  namespace: bookstore
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
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "StatefulSet",
			yaml: `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nginx
  namespace: bookstore
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
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "Job",
			yaml: `
apiVersion: batch/v1
kind: Job
metadata:
  name: nginx
  namespace: bookstore
spec:
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx:1.7.9
        name: nginx
        ports:
        - containerPort: 80
`,
		},
		{
			name: "CronJob",
			yaml: `
apiVersion: batch/v1beta1
kind: CronJob
metadata:
  name: nginx
  namespace: bookstore
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - image: nginx:1.7.9
            name: nginx
            ports:
            - containerPort: 80
`,
		},
		{
			name: "Service",
			yaml: `
apiVersion: v1
kind: Service
metadata:
  name: nginx
  namespace: bookstore
spec:
  selector:
    app: nginx
  ports:
  - port: 80
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			u := &unstructured.Unstructured{}
			_, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(tc.yaml), nil, u)
			if err != nil {
				t.Fatal(err)
			}

			converter, err := declared.ValueConverterForTest()
			if err != nil {
				t.Fatal(err)
			}

			objs := &objects.Raw{
				Converter: converter,
				Objects: []ast.FileObject{{
					Unstructured: u,
				}},
			}

			err = DeclaredFields(objs)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
