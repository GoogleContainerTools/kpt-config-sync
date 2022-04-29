// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package manifestreader

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
)

// PathManifestReader implements ManifestReader interface.
var _ ManifestReader = &PathManifestReader{}

// PathManifestReader reads manifests from the provided path
// and returns them as Info objects. The returned Infos will not have
// client or mapping set.
type PathManifestReader struct {
	Path string

	ReaderOptions
}

// Read reads the manifests and returns them as Info objects.
func (p *PathManifestReader) Read() ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	nodes, err := (&kio.LocalPackageReader{
		PackagePath: p.Path,
	}).Read()
	if err != nil {
		return objs, err
	}

	for _, n := range nodes {
		err = RemoveAnnotations(n, kioutil.IndexAnnotation)
		if err != nil {
			return objs, err
		}
		u, err := KyamlNodeToUnstructured(n)
		if err != nil {
			return objs, err
		}
		objs = append(objs, u)
	}

	objs = FilterLocalConfig(objs)

	err = SetNamespaces(p.Mapper, objs, p.Namespace, p.EnforceNamespace)
	return objs, err
}
