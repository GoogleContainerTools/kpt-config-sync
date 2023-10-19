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
	"math"
	"sync"
	"time"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fightThreshold is the threshold of updates per minute at which we log to Info
// that the Syncer is fighting over a resource on the API Server with some
// other process.
//
// This value was chosen arbitrarily as updates occurring more frequently are
// obviously problems, and we don't really care about less frequent updates.
var fightThreshold = 5.0

// SetFightThreshold updates the maximum allowed rate of updates to a resource
// per minute before we begin logging errors to the user.
func SetFightThreshold(updatesPerMinute float64) {
	fightThreshold = updatesPerMinute
}

// Detector uses a linear differential equation to estimate the frequency
// of updates to resources, then logs to klog.Warning when it detects resources
// needing updates too frequently.
//
// Instantiate with NewDetector().
//
// Performance characteristics:
//  1. Current implementation is threadsafe.
//  2. Current implementation has unbounded memory usage on the order of the
//     number of objects the Syncer updates through its lifetime.
//  3. Updating an already-tracked resource requires no memory allocations and
//     take approximately 30ns, ignoring logging time.
type Detector struct {
	// mux is a reader/writer mutual exclusion lock for the cached fights.
	mux sync.RWMutex

	// fights is a record of how much the Syncer is fighting over any given
	// API resource.
	fights map[core.ID]*fight

	fLogger *logger
}

// NewDetector instantiates a fight detector.
func NewDetector() Detector {
	return Detector{
		fights:  make(map[core.ID]*fight),
		fLogger: newLogger(),
	}
}

// DetectFight detects whether the resource is needing updates too frequently.
// If so, it increments the resource_fights metric and logs to klog.Error.
func (d *Detector) DetectFight(now time.Time, obj client.Object) (bool, status.ResourceError) {
	d.mux.Lock()
	defer d.mux.Unlock()
	id := core.IDOf(obj)

	if d.fights[id] == nil {
		d.fights[id] = &fight{}
	}
	if frequency := d.fights[id].refreshUpdateFrequency(now, true); frequency >= fightThreshold {
		fightErr := status.FightError(frequency, obj)
		return d.fLogger.logFight(now, fightErr), fightErr
	}
	return false, nil
}

// ResolveFight cools down the fight, and checks if the fight still exists.
func (d *Detector) ResolveFight(now time.Time, obj client.Object) (bool, status.ResourceError) {
	d.mux.Lock()
	defer d.mux.Unlock()
	id := core.IDOf(obj)

	if d.fights[id] == nil {
		return false, nil
	}
	if frequency := d.fights[id].refreshUpdateFrequency(now, false); frequency >= fightThreshold {
		fightErr := status.FightError(frequency, obj)
		return d.fLogger.logFight(now, fightErr), fightErr
	}
	return false, nil
}

// fight estimates how often a specific API resource is updated by the Syncer.
type fight struct {
	// heat is an estimate of the number of times a resource is updated per minute.
	// It decays exponentially with time when there are no updates to a resource.
	heat float64
	// last is the last time the resource was updated.
	last time.Time
}

// refreshUpdateFrequency advanced the time on fight to now and increases heat by 1.0.
// Returns the estimated frequency of updates per minute.
//
// Required reading to understand the math:
// https://www.math24.net/linear-differential-equations-first-order/
//
// Specifically, Detector uses the below linear differential equation to
// estimate the number of updates a resource has per minute.
//
// y' = -y + k(t)
//
// Where `y` is the estimated rate, `k` is the actual (potentially variable)
// rate of updates, and t is time. If k(t) is a constant, y(t) converges to `k`
// exponentially. When y and k(t) are approximately equal, y' remains about zero.
//
// The implementation below uses an approximation to the above as updates are
// quantized (there is no such thing as 0.5 of an update).
func (f *fight) refreshUpdateFrequency(now time.Time, update bool) float64 {
	// How long to let the existing heat decay.
	d := now.Sub(f.last).Minutes()
	// Only update `last` when it is update.
	// If no update, keep `last` unchanged for longer duration to cool down the heat.
	if update {
		f.last = now
	}
	// Don't let d be a negative number of minutes to keep heat from increasing.
	d = math.Max(0.0, d)

	// Exponential decay of heat. Using e^-d, where d is in minutes makes
	// heat exactly our estimate of number of updates per minute before this
	// update.
	f.heat *= math.Exp(-d)

	// Increment heat by one update. This is our new estimate of updates per minute.
	f.heat++
	return f.heat
}
