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

package controllers

import (
	"hash/fnv"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"
)

func hash(allData interface{}) ([]byte, error) {
	data, err := json.Marshal(allData)
	if err != nil {
		return nil, errors.Errorf("failed to marshal ConfigMaps data, error: %v", err)
	}
	h := fnv.New128()
	if n, err := h.Write(data); n < len(data) {
		return nil, errors.Errorf("failed to write configmap data, error: %v", err)
	}
	return h.Sum(nil), nil
}
