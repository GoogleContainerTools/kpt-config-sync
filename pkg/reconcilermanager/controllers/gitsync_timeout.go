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

package controllers

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func mutateGitsyncTimeout(c *corev1.Container, override int) error {
	// If no overrides are provided, return
	if override <= 0 {
		return nil
	}

	c.Args = append(c.Args, fmt.Sprintf("--timeout=%d", override))
	return nil
}
