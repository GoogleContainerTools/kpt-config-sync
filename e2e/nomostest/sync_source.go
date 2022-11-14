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

package nomostest

// SyncSource represent an in-cluster sync source component.
// e.g. git server, registry server, etc.
// These components are intended to share a lifecycle with either each testcase
// or the entire test suite.
// TODO: refactor this interface and it's implementation to a package
type SyncSource interface {
	Install()
	WaitForReady()
	PortForward() int
}

const gitSource = "git"
const registrySource = "registry"

func NewSyncSource(nt *NT, sourceType string) SyncSource {
	switch sourceType {
	case gitSource:
		return &GitServer{nt: nt}
	case registrySource:
		return &RegistryServer{nt: nt}
	default:
		nt.T.Fatal("unrecognized sourceType %s", sourceType)
	}
	return nil
}
