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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	//nolint:staticcheck
	clientgoopenapi "k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
	"kpt.dev/configsync/pkg/declared"
)

const openapiDir = "openapi-spec/v3"

func pathToSpecDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get path to %s", openapiDir)
	}
	return filepath.Join(filepath.Dir(filename), openapiDir), nil
}

type fakeOpenAPIClient struct{}

var _ clientgoopenapi.Client = &fakeOpenAPIClient{}

func (f fakeOpenAPIClient) Paths() (map[string]clientgoopenapi.GroupVersion, error) {
	paths := map[string]clientgoopenapi.GroupVersion{}
	dir, err := pathToSpecDir()
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		gv := strings.ReplaceAll(strings.Trim(file.Name(), ".json"), "_", "/")
		fullPath := filepath.Join(dir, file.Name())
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		var openapi spec3.OpenAPI
		if err := json.Unmarshal(data, &openapi); err != nil {
			return nil, err
		}
		paths[gv] = &fakeOpenAPIGroupVersion{openapi}
	}
	return paths, nil
}

type fakeOpenAPIGroupVersion struct {
	openapi spec3.OpenAPI
}

func (f *fakeOpenAPIGroupVersion) Schema(contentType string) ([]byte, error) {
	return json.Marshal(f.openapi)
}

var _ clientgoopenapi.GroupVersion = &fakeOpenAPIGroupVersion{}

// ValueConverterForTest returns a ValueConverter initialized for unit tests.
func ValueConverterForTest() (*declared.ValueConverter, error) {
	return declared.NewValueConverter(fakeOpenAPIClient{})
}
