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

package reconcile

import (
	"time"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

// fightLogger is used to log errors about fights from fightDetector at most
// once every 60 seconds. It has similar performance characteristics as
// fightDetector.
//
// Instantiate with newFightLogger().
type fightLogger struct {
	// lastLogged is when fightLogger last logged about a given API resource.
	lastLogged map[gknn]time.Time
}

func newFightLogger() fightLogger {
	return fightLogger{
		lastLogged: make(map[gknn]time.Time),
	}
}

// markUpdated marks that API resource `resource` was updated at time `now`.
// If the estimated frequency of updates is greater than `fightThreshold`, logs
// this to klog.Warning. The log message appears at most once per minute.
//
// Returns true if the new estimated update frequency is at least `fightThreshold`.
func (d *fightLogger) logFight(now time.Time, err status.ResourceError) bool {
	resource := err.Resources()[0] // There is only ever one resource per fight error.
	i := gknn{
		gk:        resource.GetObjectKind().GroupVersionKind().GroupKind(),
		namespace: resource.GetNamespace(),
		name:      resource.GetName(),
	}

	if now.Sub(d.lastLogged[i]) <= time.Minute {
		return false
	}

	klog.Warning(err)
	d.lastLogged[i] = now
	return true
}
