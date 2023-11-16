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

package v1alpha1

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var c client.Client

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "manifests")},
	}

	s := scheme.Scheme
	if err := AddToScheme(s); err != nil {
		log.Fatal(err)
	}
	if err := v1.AddToScheme(s); err != nil {
		log.Fatal(err)
	}

	cfg, err := t.Start()
	if err != nil {
		log.Fatal(err)
	}

	if c, err = client.New(cfg, client.Options{Scheme: s}); err != nil {
		log.Fatal(err)
	}

	code := m.Run()

	err = t.Stop()
	if err != nil {
		log.Printf("Error: Failed to stop test env: %v", err)
	}

	os.Exit(code)
}
