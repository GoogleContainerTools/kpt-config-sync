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
	"fmt"
	"os"
)

// PrintErr attempts to print an error to STDERR.
func PrintErr(err error) error {
	_, printErr := fmt.Fprintln(os.Stderr, err)
	return printErr
}

// PrintErrOrDie attempts to print an error to STDERR, and panics if it is unable to.
func PrintErrOrDie(err error) {
	printErr := PrintErr(err)
	if printErr != nil {
		panic(printErr)
	}
}
