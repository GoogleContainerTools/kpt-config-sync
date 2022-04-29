// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package manifestreader

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ManifestReader defines the interface for reading a set
// of manifests into info objects.
type ManifestReader interface {
	Read() ([]*unstructured.Unstructured, error)
}

// ReaderOptions defines the shared inputs for the different
// implementations of the ManifestReader interface.
type ReaderOptions struct {
	Mapper           meta.RESTMapper
	Validate         bool
	Namespace        string
	EnforceNamespace bool
}
