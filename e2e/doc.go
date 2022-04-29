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

// Package e2e and its subpacakages define go e2e tests for Nomos.
//
// Running the Tests
//
// A) With make, rebuilding the Nomos image:
//
// $ make go-e2e-test
//
// B) With make, without rebuilding the image:
//
// $ make go-e2e-test-nobuild
//
// C) Directly, without rebuilding:
//
// $ go test ./e2e/... --e2e
//
// Running the tests directly requires running scripts.docker-registry.sh as
// a one-time setup step.
//
// You can use all of the normal `go test` flags.
// The `--e2e` is required or else the e2e tests won't run. This lets you run
// go test ./... to just run unit/integration tests.
//
//
// Debugging
//
// Use --debug to use the debug mode for tests. In this mode, on failure the
// test does not destroy the kind cluster and delete the temporary directory.
// Instead, it prints out where the temporary directory is and how to connect to
// the kind cluster.
//
// The temporary directory includes:
// 1) All manifests used to install ConfigSync
// 2) The private/public SSH keys to connect to git-server
// 3) The local repository(ies), already configured to talk to git server.
//      Just remember to port-forward to the git-server Pod if you want to read
//      from/write to it.
//
// If you want to stop the test at any time, just use t.FailNow().
package e2e
