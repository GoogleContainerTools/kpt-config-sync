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
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	// The openapi Document type does not satisfy the proto.Message interface in
	// the new non-deprecated proto library.
	"github.com/golang/protobuf/proto" //nolint:staticcheck
	openapiv2 "github.com/google/gnostic/openapiv2"
	"kpt.dev/configsync/pkg/declared"
)

const openapiFile = "openapi_v2.txt"

// Doc returns an openapi Document intialized from a static file of openapi
// schemas.
func Doc() (*openapiv2.Document, error) {
	path, err := pathToTestFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := &openapiv2.Document{}
	if err := proto.Unmarshal(data, doc); err != nil {
		return nil, err
	}

	return doc, err
}

func pathToTestFile() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get path to %s", openapiFile)
	}
	return filepath.Join(filepath.Dir(filename), openapiFile), nil
}

type fakeOpenAPIInterface struct{}

func (f fakeOpenAPIInterface) OpenAPISchema() (*openapiv2.Document, error) {
	return Doc()
}

// ValueConverterForTest returns a ValueConverter initialized for unit tests.
func ValueConverterForTest() (*declared.ValueConverter, error) {
	return declared.NewValueConverter(fakeOpenAPIInterface{})
}
