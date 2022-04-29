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

package fake

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrorClient is a Client that always returns a specified error.
type ErrorClient struct {
	error error
}

// NewErrorClient returns a Client that always returns an error.
func NewErrorClient(err error) client.Client {
	return &ErrorClient{error: err}
}

// Get implements client.Client.
func (e ErrorClient) Get(_ context.Context, _ client.ObjectKey, _ client.Object) error {
	return e.error
}

// List implements client.Client.
func (e ErrorClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return e.error
}

// Create implements client.Client.
func (e ErrorClient) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	return e.error
}

// Delete implements client.Client.
func (e ErrorClient) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	return e.error
}

// Update implements client.Client.
func (e ErrorClient) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	return e.error
}

// Patch implements client.Client.
func (e ErrorClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return e.error
}

// DeleteAllOf implements client.Client.
func (e ErrorClient) DeleteAllOf(_ context.Context, _ client.Object, _ ...client.DeleteAllOfOption) error {
	return e.error
}

// Status implements client.Client.
func (e ErrorClient) Status() client.StatusWriter {
	return e
}

// Scheme implements client.Client.
func (e ErrorClient) Scheme() *runtime.Scheme {
	panic("fake.ErrorClient does not support Scheme()")
}

// RESTMapper implements client.Client.
func (e ErrorClient) RESTMapper() meta.RESTMapper {
	panic("fake.ErrorClient does not support RESTMapper()")
}
