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

package fight

import (
	"sync"
	"time"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
)

// logger is used to log errors about fights from Detector at most
// once every 60 seconds. It has similar performance characteristics as
// Detector.
//
// Instantiate with newLogger().
type logger struct {
	// mux is a reader/writer mutual exclusion lock for the cached lastLogged map.
	mux sync.RWMutex

	// lastLogged is when logger last logged about a given API resource.
	lastLogged map[core.ID]time.Time
}

// newLogger instantiates a fight logger.
func newLogger() *logger {
	return &logger{
		lastLogged: make(map[core.ID]time.Time),
	}
}

// logFight logs the fight if the estimated frequency of updates is greater than
// `fightThreshold`. The log message appears at most once per minute.
//
// Returns true if the new estimated update frequency is at least `fightThreshold`.
func (l *logger) logFight(now time.Time, err status.ResourceError) bool {
	l.mux.Lock()
	defer l.mux.Unlock()

	resource := err.Resources()[0] // There is only ever one resource per fight error.
	id := core.IDOf(resource)

	if now.Sub(l.lastLogged[id]) <= time.Minute {
		return false
	}

	klog.Error(err)
	l.lastLogged[id] = now
	return true
}
