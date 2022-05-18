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

package openapitest

import (
	"io/ioutil"
	// The openapi Document type does not satisfy the proto.Message interface in
	// the new non-deprecated proto library.
	"github.com/golang/protobuf/proto" //nolint:staticcheck
	openapiv2 "github.com/google/gnostic/openapiv2"
)

// Doc returns an openapi Document intialized from a static file of openapi
// schemas.
func Doc(path string) (*openapiv2.Document, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := &openapiv2.Document{}
	if err := proto.Unmarshal(data, doc); err != nil {
		return nil, err
	}

	return doc, err
}
