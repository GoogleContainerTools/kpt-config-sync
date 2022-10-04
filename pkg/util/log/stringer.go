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

package log

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/json"
)

type jsonStringer struct {
	O interface{}
}

// AsJSON returns a new stringer object that delays marshaling until the
// String method is called. For logging at higher verbosity levels, to
// avoid formatting when the output isn't going to be used.
func AsJSON(o interface{}) fmt.Stringer {
	return &jsonStringer{O: o}
}

// Returns the object as json, or the error string if marshalling fails.
func (ojs *jsonStringer) String() string {
	bytes, err := json.Marshal(ojs.O)
	if err != nil {
		return err.Error()
	}
	return string(bytes)
}
