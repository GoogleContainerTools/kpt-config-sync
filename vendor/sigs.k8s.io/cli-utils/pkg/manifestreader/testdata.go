// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package manifestreader

var (
	depManifest = `
kind: Deployment
apiVersion: apps/v1
metadata:
  name: dep
spec:
  replicas: 1
`

	cmManifest = `
kind: ConfigMap
apiVersion: v1
metadata:
  name: cm
data:
  foo: bar
`
)
