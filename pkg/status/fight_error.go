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

package status

import "sigs.k8s.io/controller-runtime/pkg/client"

// FightErrorCode is the error code for Config Sync fighting with other controllers.
const FightErrorCode = "2005"

var fightErrorBuilder = NewErrorBuilder(FightErrorCode)

// FightError represents when the remediator is fighting over a resource object
// with some other process on a Kubernetes cluster.
func FightError(frequency float64, resource client.Object) ResourceError {
	return fightErrorBuilder.Sprintf("detected excessive object updates, approximately %d times per minute. "+
		"This may indicate Config Sync is fighting with another controller over the object.", int(frequency)).
		BuildWithResources(resource)
}
