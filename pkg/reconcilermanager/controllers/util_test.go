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

package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

func TestHelmSyncEnvs(t *testing.T) {
	duration, _ := time.ParseDuration("15s")
	testCases := map[string]struct {
		base              v1beta1.HelmBase
		releaseNamespace  string
		deployNamespace   string
		versionPollPeriod string

		expected []corev1.EnvVar
	}{
		"with inline values": {
			base: v1beta1.HelmBase{
				Repo:        "example.com/repo",
				Chart:       "my-chart",
				Version:     "1.0.0",
				ReleaseName: "release-name",
				Values: &apiextensionsv1.JSON{
					Raw: []byte("foo: bar"),
				},
				ValuesFileApplyStrategy: "override",
				Period:                  metav1.Duration{Duration: duration},
				Auth:                    "none",
			},
			releaseNamespace:  "releaseNamespace",
			deployNamespace:   "deployNamespace",
			versionPollPeriod: "1h",

			expected: []corev1.EnvVar{
				{Name: reconcilermanager.HelmRepo, Value: "example.com/repo"},
				{Name: reconcilermanager.HelmChart, Value: "my-chart"},
				{Name: reconcilermanager.HelmChartVersion, Value: "1.0.0"},
				{Name: reconcilermanager.HelmReleaseName, Value: "release-name"},
				{Name: reconcilermanager.HelmReleaseNamespace, Value: "releaseNamespace"},
				{Name: reconcilermanager.HelmDeployNamespace, Value: "deployNamespace"},
				{Name: reconcilermanager.HelmValues, Value: "foo: bar"},
				{Name: reconcilermanager.HelmIncludeCRDs, Value: "false"},
				{Name: reconcilermanager.HelmAuthType, Value: "none"},
				{Name: reconcilermanager.HelmSyncWait, Value: "15.000000"},
				{Name: reconcilermanager.HelmSyncVersionPollingPeriod, Value: "1h"},
				{Name: reconcilermanager.HelmValuesFileApplyStrategy, Value: "override"},
			},
		},
		"without inline values": {
			base: v1beta1.HelmBase{
				Repo:                    "example.com/repo",
				Chart:                   "my-chart",
				Version:                 "1.0.0",
				ReleaseName:             "release-name",
				Period:                  metav1.Duration{Duration: duration},
				Auth:                    "none",
				ValuesFileApplyStrategy: "merge",
			},
			releaseNamespace:  "releaseNamespace",
			deployNamespace:   "deployNamespace",
			versionPollPeriod: "1h",

			expected: []corev1.EnvVar{
				{Name: reconcilermanager.HelmRepo, Value: "example.com/repo"},
				{Name: reconcilermanager.HelmChart, Value: "my-chart"},
				{Name: reconcilermanager.HelmChartVersion, Value: "1.0.0"},
				{Name: reconcilermanager.HelmReleaseName, Value: "release-name"},
				{Name: reconcilermanager.HelmReleaseNamespace, Value: "releaseNamespace"},
				{Name: reconcilermanager.HelmDeployNamespace, Value: "deployNamespace"},
				{Name: reconcilermanager.HelmValues, Value: ""},
				{Name: reconcilermanager.HelmIncludeCRDs, Value: "false"},
				{Name: reconcilermanager.HelmAuthType, Value: "none"},
				{Name: reconcilermanager.HelmSyncWait, Value: "15.000000"},
				{Name: reconcilermanager.HelmSyncVersionPollingPeriod, Value: "1h"},
				{Name: reconcilermanager.HelmValuesFileApplyStrategy, Value: "merge"},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, helmSyncEnvs(&tc.base, tc.releaseNamespace, tc.deployNamespace, tc.versionPollPeriod))
		})
	}
}
