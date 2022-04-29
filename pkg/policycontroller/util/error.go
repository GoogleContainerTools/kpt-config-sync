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

import "fmt"

// PolicyControllerError is the shared error schema for Gatekeeper constraints
// and constraint templates.
type PolicyControllerError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

// FormatErrors flattens the given errors into a string array.
func FormatErrors(id string, pces []PolicyControllerError) []string {
	var errs []string
	for _, pce := range pces {
		var prefix string
		if len(pce.Code) > 0 {
			prefix = fmt.Sprintf("[%s] %s:", id, pce.Code)
		} else {
			prefix = fmt.Sprintf("[%s]:", id)
		}

		msg := pce.Message
		if len(msg) == 0 {
			msg = "[missing PolicyController error]"
		}

		errs = append(errs, fmt.Sprintf("%s %s", prefix, msg))
	}
	return errs
}
