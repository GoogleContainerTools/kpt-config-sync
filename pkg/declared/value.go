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

package declared

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/openapi"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

// ValueConverter converts a runtime.Object into a TypedValue.
type ValueConverter struct {
	oc             openapi.Client
	typedConverter managedfields.TypeConverter
}

// NewValueConverter returns a ValueConverter initialized with the given
// discovery client.
func NewValueConverter(oc openapi.Client) (*ValueConverter, error) {
	v := &ValueConverter{oc: oc}
	if err := v.Refresh(); err != nil {
		return nil, err
	}
	return v, nil
}

// Refresh pulls fresh schemas from the openapi discovery endpoint and
// instantiates the ValueConverter with them. This can be called periodically as
// new custom types (eg CRDs) are added to the cluster.
func (v *ValueConverter) Refresh() error {
	typeConverter, err := openapi.NewTypeConverter(v.oc, false)
	if err != nil {
		return fmt.Errorf("failed to create typedConverter: %w", err)
	}

	v.typedConverter = typeConverter
	return nil
}

// TypedValue returns the equivalent TypedValue for the given Object.
func (v *ValueConverter) TypedValue(obj runtime.Object) (*typed.TypedValue, error) {
	typedValue, err := v.typedConverter.ObjectToTyped(obj)
	if err == nil {
		return typedValue, nil
	}
	return managedfields.NewDeducedTypeConverter().ObjectToTyped(obj)
}
