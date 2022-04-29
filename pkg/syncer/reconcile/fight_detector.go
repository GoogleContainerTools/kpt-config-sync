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
	ctx "context"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	m "kpt.dev/configsync/pkg/metrics"
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
// per minute before we begin logging warnings to the user.
func SetFightThreshold(updatesPerMinute float64) {
	fightThreshold = updatesPerMinute
}

var fightWarningBuilder = status.NewErrorBuilder("2005")

// FightWarning represents when the Syncer is fighting over a resource with
// some other process on a Kubernetes cluster.
func FightWarning(frequency float64, resource client.Object) status.ResourceError {
	return fightWarningBuilder.Sprintf("syncer excessively updating resource, approximately %d times per minute. "+
		"This may indicate ACM is fighting with another controller over the resource.", int(frequency)).
		BuildWithResources(resource)
}

// fightDetector uses a linear differential equation to estimate the frequency
// of updates to resources, then logs to klog.Warning when it detects resources
// needing updates too frequently.
//
// Instantiate with newFightDetector().
//
// Performance characteristics:
// 1. Current implementation is NOT threadsafe.
// 2. Current implementation has unbounded memory usage on the order of the
//   number of objects the Syncer updates through its lifetime.
// 3. Updating an already-tracked resource requires no memory allocations and
//   take approximately 30ns, ignoring logging time.
type fightDetector struct {
	// fights is a record of how much the Syncer is fighting over any given
	// API resource.
	fights map[gknn]*fight
}

func newFightDetector() fightDetector {
	return fightDetector{
		fights: make(map[gknn]*fight),
	}
}

// detectFight detects whether the resource is needing updates too frequently.
// If so, it increments the resource_fights metric and logs to klog.Warning.
func (d *fightDetector) detectFight(ctx ctx.Context, time time.Time, obj *unstructured.Unstructured, fLogger *fightLogger, operation string) bool {
	if fight := d.markUpdated(time, obj); fight != nil {
		m.RecordResourceFight(ctx, operation, obj.GroupVersionKind())
		if fLogger.logFight(time, fight) {
			return true
		}
	}
	return false
}

// markUpdated marks that API resource `resource` was updated at time `now`.
// Returns a ResourceError if the estimated frequency of updates is greater than
// `fightThreshold`.
func (d *fightDetector) markUpdated(now time.Time, resource client.Object) status.ResourceError {
	i := gknn{
		gk:        resource.GetObjectKind().GroupVersionKind().GroupKind(),
		namespace: resource.GetNamespace(),
		name:      resource.GetName(),
	}

	if d.fights[i] == nil {
		d.fights[i] = &fight{}
	}
	if frequency := d.fights[i].markUpdated(now); frequency >= fightThreshold {
		return FightWarning(frequency, resource)
	}
	return nil
}

// gknn uniquely identifies a resource on the API Server with the resource's
// Group, Kind, Namespace, and Name.
type gknn struct {
	// Recall that two resources identical except for Version are the same
	// resource.
	gk              schema.GroupKind
	namespace, name string
}

// fight estimates how often a specific API resource is updated by the Syncer.
type fight struct {
	// heat is an estimate of the number of times a resource is updated per minute.
	// It decays exponentially with time when there are no updates to a resource.
	heat float64
	// last is the last time the resource was updated.
	last time.Time
}

// markUpdated advanced the time on fight to now and increases heat by 1.0.
// Returns the estimated frequency of updates per minute.
//
// Required reading to understand the math:
// https://www.math24.net/linear-differential-equations-first-order/
//
// Specifically, fightDetector uses the below linear differential equation to
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
func (f *fight) markUpdated(now time.Time) float64 {
	// How long to let the existing heat decay.
	d := now.Sub(f.last).Minutes()
	f.last = now
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
