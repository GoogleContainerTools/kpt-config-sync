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

// Package decode is used for decoding serialized data in Nomos resources.
package decode

import (
	"fmt"

	"kpt.dev/configsync/pkg/syncer/scheme"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
)

// Decoder decodes GenericResources from NamespaceConfigs / ClusterConfigs to
// Unstructured structs.
type Decoder interface {
	// DecodeResources reads the bytes in the RawExtensions representing k8s
	// resources and returns a slice of all the resources grouped by their
	// respective GroupVersionKind.
	DecodeResources(genericResources []v1.GenericResources) (map[schema.GroupVersionKind][]*unstructured.Unstructured, error)
	// UpdateScheme updates the scheme of the underlying decoder, so it can decode the given GroupVersionKinds.
	UpdateScheme(gvks map[schema.GroupVersionKind]bool)
}

var _ Decoder = &genericResourceDecoder{}

// genericResourceDecoder implements Decoder.
type genericResourceDecoder struct {
	scheme                *runtime.Scheme
	decoder               runtime.Decoder
	unstructuredConverter runtime.UnstructuredConverter
}

// NewGenericResourceDecoder returns a new genericResourceDecoder.
func NewGenericResourceDecoder(scheme *runtime.Scheme) Decoder {
	return &genericResourceDecoder{
		scheme:                scheme,
		decoder:               serializer.NewCodecFactory(scheme).UniversalDeserializer(),
		unstructuredConverter: runtime.DefaultUnstructuredConverter,
	}
}

// UpdateScheme implements Decoder.
func (d *genericResourceDecoder) UpdateScheme(gvks map[schema.GroupVersionKind]bool) {
	scheme.AddToSchemeAsUnstructured(d.scheme, gvks)
	d.decoder = serializer.NewCodecFactory(d.scheme).UniversalDeserializer()
}

// DecodeResources implements Decoder.
func (d *genericResourceDecoder) DecodeResources(genericResources []v1.GenericResources) (map[schema.GroupVersionKind][]*unstructured.Unstructured, error) {
	us := make(map[schema.GroupVersionKind][]*unstructured.Unstructured)
	for _, gr := range genericResources {
		for _, v := range gr.Versions {
			for _, genericObject := range v.Objects {
				gvk := schema.GroupVersionKind{Group: gr.Group, Version: v.Version, Kind: gr.Kind}
				o := genericObject.Object
				if o == nil {
					u := &unstructured.Unstructured{}
					var err error
					o, _, err = d.decoder.Decode(genericObject.Raw, &gvk, u)
					if err != nil {
						return nil, errors.Wrapf(err, "could not decode client.Object from %q RawExtension bytes", gvk)
					}
				}
				au, ok := o.(*unstructured.Unstructured)
				if !ok {
					m, err := d.unstructuredConverter.ToUnstructured(o)
					if err != nil {
						return nil, fmt.Errorf("could not treat GenericResource object %q as an unstructured.Unstructured", gvk)
					}
					au = &unstructured.Unstructured{Object: m}
					au.SetGroupVersionKind(gvk)
				}
				us[gvk] = append(us[gvk], au)
			}
		}
	}
	return us, nil
}
