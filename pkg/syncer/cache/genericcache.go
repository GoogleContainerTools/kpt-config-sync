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

// Package cache includes controller caches.
package cache

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GenericCache wraps a client.Reader to add a single convenience method,
// UnstructuredList, that returns a slice of *unstructured.Unstructured.
type GenericCache struct {
	client.Reader
}

// NewGenericResourceCache returns a new GenericCache.
func NewGenericResourceCache(reader client.Reader) *GenericCache {
	return &GenericCache{Reader: reader}
}

// UnstructuredList returns all the resources in the cluster of the given
// GroupVersionKind for the given namespace. If the namespace is empty, it
// will return all resources across all namespaces. Namespace should always
// be set to empty when listing cluster-scoped resources.
// This method is needed because cache.Cache's List method requires knowing
// the type of the resource you wanted to list. We always want to return
// Unstructureds when listing resources on the cluster, whether it's a native
// or custom resource.
func (c *GenericCache) UnstructuredList(ctx context.Context, gvk schema.GroupVersionKind,
	opts ...client.ListOption) ([]*unstructured.Unstructured, error) {
	if !strings.HasSuffix(gvk.Kind, "List") {
		gvk.Kind = gvk.Kind + "List"
	}

	ul := &unstructured.UnstructuredList{}
	ul.SetGroupVersionKind(gvk)
	err := c.List(ctx, ul, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "fetching UnstructuredList for %s", gvk)
	}

	// The existing API uses arrays of pointers to Unstructureds; Items is actual structs
	// we oblige and convert here for the return array.
	var uls []*unstructured.Unstructured
	for i := 0; i < len(ul.Items); i++ {
		uls = append(uls, &ul.Items[i])
	}

	return uls, nil
}
