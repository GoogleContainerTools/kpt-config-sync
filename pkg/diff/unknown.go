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

package diff

import "sigs.k8s.io/controller-runtime/pkg/client"

type unknown struct {
	client.Object
}

var theUnknown = &unknown{}

// Unknown returns a sentinel Object which represents unknown state on the
// cluster. On failing to retrieve the current state of an Object on the
// cluster, a caller should use this to indicate that no action should be
// taken to reconcile the declared version of an Object.
func Unknown() client.Object {
	return theUnknown
}

// IsUnknown returns true if the given Object is the sentinel marker of unknown
// state on the cluster.
func IsUnknown(obj client.Object) bool {
	return obj == theUnknown
}
