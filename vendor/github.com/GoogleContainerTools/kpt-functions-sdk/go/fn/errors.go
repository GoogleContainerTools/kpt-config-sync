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

package fn

import (
	"fmt"
	"strings"
)

// ErrMissingFnConfig raises error if a required functionConfig is missing.
type ErrMissingFnConfig struct{}

func (ErrMissingFnConfig) Error() string {
	return "unable to find the functionConfig in the resourceList"
}

// errKubeObjectFields raises if the KubeObject operation panics.
type errKubeObjectFields struct {
	obj    *KubeObject
	fields []string
}

func (e *errKubeObjectFields) Error() string {
	return fmt.Sprintf("Resource(apiVersion=%v, kind=%v, Name=%v) has unmatched field type: `%v",
		e.obj.GetAPIVersion(), e.obj.GetKind(), e.obj.GetName(), strings.Join(e.fields, "/"))
}

// errSubObjectFields raises if the SubObject operation panics.
type errSubObjectFields struct {
	fields []string
}

func (e *errSubObjectFields) Error() string {
	return fmt.Sprintf("SubObject has unmatched field type: `%v", strings.Join(e.fields, "/"))
}

type errResultEnd struct {
	obj     *KubeObject
	message string
}

func (e *errResultEnd) Error() string {
	if e.obj != nil {
		return fmt.Sprintf("function is terminated by %v: %v", e.obj.ShortString(), e.message)
	}
	return fmt.Sprintf("function is terminated: %v", e.message)
}

type ErrAttemptToTouchUpstreamIdentifier struct{}

func (ErrAttemptToTouchUpstreamIdentifier) Error() string {
	return fmt.Sprintf("annotation %v is managed by kpt and should not be modified", UpstreamIdentifier)
}

type ErrInternalAnnotation struct {
	Message string
}

func (e *ErrInternalAnnotation) Error() string {
	return e.Message
}
