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

package declared

import (
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/pkg/status"
)

// Scope defines a distinct (but not necessarily disjoint) area of responsibility
// for a Reconciler.
type Scope string

// RootReconciler is a special constant for a Scope for a reconciler which is
// running as the "root reconciler" (vs a namespace reconciler).
//
// This Scope takes precedence over all others.
const RootReconciler = Scope(":root")

// ValidateScope ensures the passed string is either the special RootReconciler value
// or is a valid Namespace name.
func ValidateScope(s string) error {
	if s == string(RootReconciler) {
		return nil
	}
	errs := validation.IsDNS1123Subdomain(s)
	if len(errs) > 0 {
		return status.InternalErrorf("invalid scope %q: %v", s, errs)
	}
	return nil
}
