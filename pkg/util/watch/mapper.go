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

package watch

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// ResettableRESTMapper is a RESTMapper which is capable of resetting itself
// from discovery.
//
// This interface replaces meta.ResettableRESTMapper so that Reset can return an
// error. If Reset errors, it may be retried.
// This interface does NOT work with meta.MaybeResetRESTMapper.
//
// TODO: Only reset one resource at a time, for efficiency.
type ResettableRESTMapper interface {
	meta.RESTMapper
	Reset() error
}

// MapperFunc returns a RESTMapper with an empty/reset cache or an error.
type MapperFunc func() (meta.RESTMapper, error)

type mapper struct {
	mux        sync.RWMutex
	mapper     meta.RESTMapper
	mapperFunc MapperFunc
}

var _ ResettableRESTMapper = &mapper{}

// ReplaceOnResetRESTMapperFromConfig builds a ResettableRESTMapper using a
// DynamicRESTMapper that is replaced when Reset is called.
//
// DynamicRESTMapper dynamically and transparently discovers new resources, when
// a NoMatchFound error is encountered, but it doesn't automatically invalidate
// deleted resources. So use ReplaceOnResetRESTMapper to replace the whole
// DynamicRESTMapper when Reset is called.
func ReplaceOnResetRESTMapperFromConfig(cfg *rest.Config) (ResettableRESTMapper, error) {
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating HTTPClient: %w", err)
	}
	initialMapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating DynamicRESTMapper: %w", err)
	}
	newMapperFn := func() (meta.RESTMapper, error) {
		m, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
		if err != nil {
			return nil, fmt.Errorf("creating DynamicRESTMapper: %w", err)
		}
		return m, nil
	}
	return NewReplaceOnResetRESTMapper(initialMapper, newMapperFn), nil
}

// NewReplaceOnResetRESTMapper wraps the provided RESTMapper and replaces it
// using the MapperFunc when the Reset method is called.
func NewReplaceOnResetRESTMapper(m meta.RESTMapper, newMapperFunc MapperFunc) ResettableRESTMapper {
	return &mapper{
		mapper:     m,
		mapperFunc: newMapperFunc,
	}
}

// KindFor delegates to mapper.KindFor.
func (m *mapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return m.getMapper().KindFor(resource)
}

// KindsFor delegates to mapper.KindsFor.
func (m *mapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return m.getMapper().KindsFor(resource)
}

// RESTMapping delegates to mapper.RESTMapping.
func (m *mapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	return m.getMapper().RESTMapping(gk, versions...)
}

// RESTMappings delegates to mapper.RESTMappings.
func (m *mapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	return m.getMapper().RESTMappings(gk, versions...)
}

// ResourceFor delegates to mapper.ResourcesFor.
func (m *mapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return m.getMapper().ResourceFor(input)
}

// ResourcesFor delegates to mapper.ResourcesFor.
func (m *mapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return m.getMapper().ResourcesFor(input)
}

// ResourceSingularizer delegates to mapper.ResourceSingularizer.
func (m *mapper) ResourceSingularizer(resource string) (singular string, err error) {
	return m.getMapper().ResourceSingularizer(resource)
}

// Reset replaces the old mapper with a new one.
func (m *mapper) Reset() error {
	m.mux.Lock()
	defer m.mux.Unlock()
	mapper, err := m.mapperFunc()
	if err != nil {
		return err
	}
	m.mapper = mapper
	return nil
}

func (m *mapper) getMapper() meta.RESTMapper {
	m.mux.RLock()
	defer m.mux.RUnlock()
	return m.mapper
}
