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

package vettesting

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/status"
)

// ExpectErrors adds an error to testing if the expected and actual errors don't match.
// Does not verify the ordering of errors.
func ExpectErrors(expected []string, err error, t *testing.T) {
	t.Helper()
	actual := errorCodeMap(err)
	if diff := cmp.Diff(toMap(expected), actual); diff != "" {
		// All of expected, err and diff are needed to debug this error message
		// effectively when it happens.
		t.Fatalf("expected:\n%v\nactual:\n%v\ndiff:\n%v", expected, err, diff)
	}
}

func toMap(codes []string) map[string]int {
	if len(codes) == 0 {
		return nil
	}

	result := make(map[string]int)
	for _, code := range codes {
		result[code] = result[code] + 1
	}
	return result
}

// ErrorCodeMap returns a map from each error code present to the number of times it occurred.
func errorCodeMap(err error) map[string]int {
	return toMap(errorCodes(err))
}

// ErrorCodes returns the KNV error codes present in the passed error
func errorCodes(err error) []string {
	switch e := err.(type) {
	case nil:
		return []string{}
	case status.Error:
		return []string{e.Code()}
	case status.MultiError:
		if e == nil {
			return []string{}
		}
		var result []string
		for _, er := range e.Errors() {
			result = append(result, errorCodes(er)...)
		}
		return result
	default:
		// For errors without a specific code
		return []string{undefinedErrorCode}
	}
}

// UndefinedErrorCode is the code representing an unregistered error. These should be eliminated.
const undefinedErrorCode = "????"
