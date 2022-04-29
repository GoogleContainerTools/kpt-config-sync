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

package bugreport

import (
	"fmt"
)

var _ error = &brNSError{}

type errName string

const (
	missingNamespace = errName("MissingNamespace")
	notManagedByACM  = errName("NotManagedByACM")
)

func errorIs(err error, name errName) bool {
	w, ok := err.(*wrappedError)
	for ok {
		err = w.child
		w, ok = err.(*wrappedError)
	}
	e, ok := err.(*brNSError)
	if !ok {
		return false
	}
	return e.Name() == name
}

type brNSError struct {
	msg       string
	name      errName
	namespace string
}

func (e brNSError) Error() string {
	return e.msg
}

func (e *brNSError) Name() errName { return e.name }

func (e *brNSError) Namespace() string { return e.namespace }

func newMissingNamespaceError(ns string) *brNSError {
	return &brNSError{msg: fmt.Sprintf("no namespace found named %s", ns), name: missingNamespace, namespace: ns}
}

func newNotManagedNamespaceError(ns string) *brNSError {
	return &brNSError{msg: fmt.Sprintf("namespace %s not managed by Anthos Config Management", ns), name: notManagedByACM, namespace: ns}
}

var _ error = &wrappedError{}

type wrappedError struct {
	child error
	msg   string
}

func (w *wrappedError) Error() string {
	return fmt.Sprintf("%s: %s", w.msg, w.child.Error())
}

func wrap(e error, msg string, args ...interface{}) *wrappedError {
	return &wrappedError{child: e, msg: fmt.Sprintf(msg, args...)}
}
