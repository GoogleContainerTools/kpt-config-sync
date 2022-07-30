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

import (
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var emptyTime = metav1.Time{}

// IgnoreTimestampUpdates ignores timestamps when testing equality, unless one
// is empty and the other is not.
//
// This is used to test R*Sync equality, because the timestamps get updated
// every time, even if nothing else changed.
var IgnoreTimestampUpdates = cmp.Comparer(func(x, y metav1.Time) bool {
	return x == emptyTime && y == emptyTime ||
		x != emptyTime && y != emptyTime
})
