// Copyright 2025 Google LLC
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

package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSyncContainerBackoff(t *testing.T) {
	testCases := []struct {
		name       string
		pollPeriod time.Duration
		wantCap    time.Duration
	}{
		{
			name:       "pollPeriod less than default backoff cap",
			pollPeriod: 1 * time.Second,
			wantCap:    1 * time.Hour,
		},
		{
			name:       "pollPeriod greater than default backoff cap",
			pollPeriod: 24 * time.Hour,
			wantCap:    24 * time.Hour,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantCap, SyncContainerBackoff(tc.pollPeriod).Cap)
		})
	}
}
