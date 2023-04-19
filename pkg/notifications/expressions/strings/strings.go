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

package strings

import (
	"crypto/sha1"
	"encoding/base64"
	"strings"

	"k8s.io/klog/v2"
)

// New returns the expression functions for "strings"
// this is a set of helper functions for strings
func New() map[string]interface{} {
	return map[string]interface{}{
		"Join": join,
		"Hash": hash,
	}
}

func join(arr interface{}, sep string) string {
	stringArr, ok := arr.([]string)
	if ok {
		return strings.Join(stringArr, sep)
	}
	// try to convert []interface{} to []string
	interfaceArr, ok := arr.([]interface{})
	if !ok {
		klog.Warningf("expected array but got %T", arr)
		return ""
	}
	stringArr = make([]string, len(interfaceArr))
	for i, val := range interfaceArr {
		stringElement, ok := val.(string)
		if ok {
			stringArr[i] = stringElement
		} else {
			klog.Warningf("expected string elements but got %T", val)
			return ""
		}
	}
	return strings.Join(stringArr, sep)
}

func hash(input string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(input))
	hash := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return hash
}
