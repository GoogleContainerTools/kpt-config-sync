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

package ntopts

// TestType represents the test type.
type TestType struct {
	// LoadTest specifies the test is a load test.
	LoadTest bool

	// StressTest specifies the test is a stress test.
	StressTest bool

	// KccTest specifies the test is for KCC resources.
	KccTest bool

	// GCENodeTest specifies the test is for verifying the gcenode auth type.
	// It requires a GKE cluster with workload identity disabled.
	GCENodeTest bool
}

// LoadTest specifies the test is a load test.
func LoadTest(opt *New) {
	opt.LoadTest = true
}

// StressTest specifies the test is a stress test.
func StressTest(opt *New) {
	opt.StressTest = true
}

// KccTest specifies the test is a kcc test.
func KccTest(opt *New) {
	opt.KccTest = true
}

// GCENodeTest specifies the test is for verifying the gcenode auth type.
func GCENodeTest(opt *New) {
	opt.GCENodeTest = true
}
