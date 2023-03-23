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

package util

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const configMapRef = "test-notification-cm"
const secretRef = "test-notification-secret"
const namespace = "bookinfo"

func notificationConfigMapWithSubscription() *corev1.ConfigMap {
	cm := fake.ConfigMapObject(core.Name(configMapRef), core.Namespace(namespace))
	cm.Data = map[string]string{
		MultiSubscriptionsField: `- recipients
  - email:user@example.com
  triggers:
    - on-sync-synced`,
	}
	return cm
}

func TestNotificationEnabled(t *testing.T) {
	testCases := []struct {
		description        string
		namespace          string
		annotations        map[string]string
		notificationConfig *v1beta1.NotificationConfig
		objects            []client.Object
		wantError          error
		wantEnabled        bool
	}{
		{
			description:        "Notifications are disabled",
			namespace:          namespace,
			annotations:        nil,
			notificationConfig: nil,
			wantError:          nil,
			wantEnabled:        false,
		},
		{
			description: "Notifications subscription provided without notificationConfig",
			namespace:   namespace,
			annotations: map[string]string{
				fmt.Sprintf("%s/subscribe.on-sync-synced.local", AnnotationsPrefix): "",
			},
			notificationConfig: nil,
			wantError:          fmt.Errorf("subscription annotation was provided without a notificationConfig"),
			wantEnabled:        false,
		},
		{
			description:        "notificationConfig provided without configMapRef and without annotation",
			namespace:          namespace,
			annotations:        nil,
			notificationConfig: &v1beta1.NotificationConfig{},
			wantError:          nil,
			wantEnabled:        false,
		},
		{
			description: "notificationConfig provided without configMapRef and with annotation",
			namespace:   namespace,
			annotations: map[string]string{
				fmt.Sprintf("%s/subscribe.on-sync-synced.local", AnnotationsPrefix): "",
			},
			notificationConfig: &v1beta1.NotificationConfig{},
			wantError:          fmt.Errorf("notificationConfig must have a configMapRef"),
			wantEnabled:        false,
		},
		{
			description: "Notification configMapRef provided but ConfigMap not found",
			namespace:   namespace,
			annotations: nil,
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
			},
			wantError:   fmt.Errorf("notification ConfigMap test-notification-cm not found in the bookinfo namespace"),
			wantEnabled: false,
		},
		{
			description: "notificationConfig provided without any subscriptions",
			namespace:   namespace,
			annotations: nil,
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
			},
			objects: []client.Object{
				fake.ConfigMapObject(core.Name(configMapRef), core.Namespace(namespace)),
			},
			wantError:   nil,
			wantEnabled: false,
		},
		{
			description: "Notification secretRef provided but Secret not found",
			namespace:   namespace,
			annotations: map[string]string{
				fmt.Sprintf("%s/subscribe.on-sync-synced.local", AnnotationsPrefix): "",
			},
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
				SecretRef: &v1beta1.SecretReference{
					Name: secretRef,
				},
			},
			objects: []client.Object{
				fake.ConfigMapObject(core.Name(configMapRef), core.Namespace(namespace)),
			},
			wantError:   fmt.Errorf("notification Secret test-notification-secret not found in the bookinfo namespace"),
			wantEnabled: false,
		},
		{
			description: "Notification properly configured with annotation based subscription",
			namespace:   namespace,
			annotations: map[string]string{
				fmt.Sprintf("%s/subscribe.on-sync-synced.local", AnnotationsPrefix): "",
			},
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
				SecretRef: &v1beta1.SecretReference{
					Name: secretRef,
				},
			},
			objects: []client.Object{
				fake.ConfigMapObject(core.Name(configMapRef), core.Namespace(namespace)),
				fake.SecretObject(secretRef, core.Namespace(namespace)),
			},
			wantError:   nil,
			wantEnabled: true,
		},
		{
			description: "Notification properly configured with ConfigMap based subscription",
			namespace:   namespace,
			annotations: nil,
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
				SecretRef: &v1beta1.SecretReference{
					Name: secretRef,
				},
			},
			objects: []client.Object{
				notificationConfigMapWithSubscription(),
				fake.SecretObject(secretRef, core.Namespace(namespace)),
			},
			wantError:   nil,
			wantEnabled: true,
		},
		{
			description: "Notification properly configured with combination of subscriptions",
			namespace:   namespace,
			annotations: map[string]string{
				fmt.Sprintf("%s/subscribe.on-sync-synced.local", AnnotationsPrefix): "",
			},
			notificationConfig: &v1beta1.NotificationConfig{
				ConfigMapRef: &v1beta1.ConfigMapReference{
					Name: configMapRef,
				},
				SecretRef: &v1beta1.SecretReference{
					Name: secretRef,
				},
			},
			objects: []client.Object{
				notificationConfigMapWithSubscription(),
				fake.SecretObject(secretRef, core.Namespace(namespace)),
			},
			wantError:   nil,
			wantEnabled: true,
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			fakeClient := syncerFake.NewClient(t, core.Scheme, tc.objects...)
			enabled, err := NotificationEnabled(ctx, fakeClient, tc.namespace, tc.annotations, tc.notificationConfig)
			if !reflect.DeepEqual(err, tc.wantError) {
				t.Errorf("NotificationEnabled() got error: %q, want error %q", err, tc.wantError)
			}
			if enabled != tc.wantEnabled {
				t.Errorf("NotificationEnabled() got enabled: %v, want error %v", enabled, tc.wantEnabled)
			}
		})
	}
}
