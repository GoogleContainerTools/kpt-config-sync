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

package util

import (
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// IsRequestTooLargeError determines whether `err` was caused by a large request.
//
// References:
//  1) https://github.com/kubernetes/kubernetes/issues/74600
//  2) https://github.com/kubernetes/kubernetes/blob/b0bc8adbc2178e15872f9ef040355c51c45d04bb/test/integration/controlplane/synthetic_controlplane_test.go#L310
func IsRequestTooLargeError(err error) bool {
	if err == nil {
		return false
	}

	// apierrors.IsRequestEntityTooLargeError(err) is true if the request size is over 3MB
	if apierrors.IsRequestEntityTooLargeError(err) {
		return true
	}

	// the error message includes `rpc error: code = ResourceExhausted desc = trying to send message larger than max` if the request size is over 2MB
	expectedMsgFor2MB := `rpc error: code = ResourceExhausted desc = trying to send message larger than max`
	if strings.Contains(err.Error(), expectedMsgFor2MB) {
		return true
	}

	// the error message includes `etcdserver: request is too large` if the request size is over 1MB
	expectedMsgFor1MB := `etcdserver: request is too large`
	return strings.Contains(err.Error(), expectedMsgFor1MB)
}
