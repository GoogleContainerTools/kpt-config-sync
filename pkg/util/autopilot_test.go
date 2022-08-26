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

package util

import (
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func setupFakeClient(t *testing.T, objs []client.Object) *syncerFake.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := admissionregistrationv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	return syncerFake.NewClient(t, s, objs...)
}

func TestIsGKEAutopilotCluster(t *testing.T) {
	testCases := []struct {
		name        string
		objects     []client.Object
		isAutopilot bool
	}{
		{
			name:        "standard cluster: has non-autogke node and no autopilot webhooks",
			objects:     []client.Object{fake.NodeObject("gke-test-default-pool-29f1a451-21lg")},
			isAutopilot: false,
		},
		{
			name:        "autopilot cluster: has autogke node and no autopilot webhooks",
			objects:     []client.Object{fake.NodeObject("gk3-test-default-pool-29f1a451-21lg")},
			isAutopilot: true,
		},
		{
			name:        "autopilot cluster: has autogke node and no autopilot webhooks",
			objects:     []client.Object{fake.NodeObject("gk3-test-default-pool-29f1a451-21lg")},
			isAutopilot: true,
		},
		{
			name:        "autopilot cluster: has the legacy policycontrollerv2 webhook, but no node",
			objects:     []client.Object{fake.ValidatingWebhookObject("policycontrollerv2.config.common-webhooks.networking.gke.io")},
			isAutopilot: true,
		},
		{
			name:        "autopilot cluster: has the new gkepolicy webhook, but no node",
			objects:     []client.Object{fake.ValidatingWebhookObject("gkepolicy.config.common-webhooks.networking.gke.io")},
			isAutopilot: true,
		},
		{
			name:        "autopilot cluster: has the legacy policycontrollerv2 webhook and a autogke node",
			objects:     []client.Object{fake.ValidatingWebhookObject("policycontrollerv2.config.common-webhooks.networking.gke.io"), fake.NodeObject("gk3-test-default-pool-29f1a451-21lg")},
			isAutopilot: true,
		},
		{
			name:        "autopilot cluster: has the new gkepolicy webhook and an autogke node",
			objects:     []client.Object{fake.ValidatingWebhookObject("gkepolicy.config.common-webhooks.networking.gke.io"), fake.NodeObject("gk3-test-default-pool-29f1a451-21lg")},
			isAutopilot: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := setupFakeClient(t, tc.objects)
			if got, err := IsGKEAutopilotCluster(fakeClient); err != nil {
				t.Errorf("%s: got unexpected error: %v", tc.name, err)
			} else if got != tc.isAutopilot {
				t.Errorf("%s: expect isAutopilot to return %t, got %t", tc.name, tc.isAutopilot, got)
			}
		})
	}
}
