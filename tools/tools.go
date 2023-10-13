//go:build tools
// +build tools

// Copyright 2023 Google LLC
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

// This file will never be built, but `go mod tidy` will see the packages
// imported here as dependencies and not remove them from `go.mod`.

package main

import (
	// krane is used in e2e tests to package and push OCI images.
	// Since we are only using krane for testing, we don't need to fork it
	// and set up an independent build pipeline, like we do for helm/kustomize.
	// Instead, we can just vendor the source and build it locally.
	_ "github.com/google/go-containerregistry/cmd/krane"
)
