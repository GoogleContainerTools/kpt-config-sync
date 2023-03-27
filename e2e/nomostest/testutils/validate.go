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

package testutils

import (
	"strings"

	"github.com/pkg/errors"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/util/log"
)

// ValidateError returns true if the specified errors contain an error
// with the specified error code and (partial) message.
func ValidateError(errs []v1beta1.ConfigSyncError, code, message string) error {
	if len(errs) == 0 {
		return errors.Errorf("no errors present")
	}
	for _, e := range errs {
		if e.Code == code {
			if message == "" || strings.Contains(e.ErrorMessage, message) {
				return nil
			}
		}
	}
	if message != "" {
		return errors.Errorf("error %s not present with message %q: %s", code, message, log.AsJSON(errs))
	}
	return errors.Errorf("error %s not present: %s", code, log.AsJSON(errs))
}
