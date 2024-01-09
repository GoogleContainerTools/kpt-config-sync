// Copyright 2024 Google LLC
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

package controllers

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metrics"
)

func TestMutateContainerLogLevelOtelAgent(t *testing.T) {
	testCases := map[string]struct {
		logLevel      int
		expectedLevel string
	}{
		"otel-agent with 0 log level": {
			logLevel:      0,
			expectedLevel: "fatal",
		},
		"otel-agent with 1 log level": {
			logLevel:      1,
			expectedLevel: "panic",
		},
		"otel-agent with 2 log level": {
			logLevel:      2,
			expectedLevel: "dpanic",
		},
		"otel-agent with 3 log level": {
			logLevel:      3,
			expectedLevel: "error",
		},
		"otel-agent with 4 log level": {
			logLevel:      4,
			expectedLevel: "warn",
		},
		"otel-agent with 5 log level": {
			logLevel:      5,
			expectedLevel: "info",
		},
		"otel-agent with 6 log level": {
			logLevel:      6,
			expectedLevel: "debug",
		},
		"otel-agent with 7 log level": {
			logLevel:      7,
			expectedLevel: "debug",
		},
		"otel-agent with 8 log level": {
			logLevel:      8,
			expectedLevel: "debug",
		},
		"otel-agent with 9 log level": {
			logLevel:      9,
			expectedLevel: "debug",
		},
		"otel-agent with 10 log level": {
			logLevel:      10,
			expectedLevel: "debug",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			container := &corev1.Container{
				Name: metrics.OtelAgentName,
			}
			override := []v1beta1.ContainerLogLevelOverride{
				{
					ContainerName: metrics.OtelAgentName,
					LogLevel:      tc.logLevel,
				},
			}
			err := mutateContainerLogLevel(container, override)
			assert.NoError(t, err)
			expectedArgs := []string{fmt.Sprintf("--set=service.telemetry.logs.level=%s", tc.expectedLevel)}
			assert.Equal(t, expectedArgs, container.Args)
		})
	}
}
