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

package notifications

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/argoproj/notifications-engine/pkg/controller"
	"github.com/argoproj/notifications-engine/pkg/services"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

func TestCreateNotificationStatus(t *testing.T) {
	testCases := []struct {
		description    string
		generation     int64
		commit         string
		eventSequence  controller.NotificationEventSequence
		expectedStatus v1beta1.NotificationStatus // omit timestamp
	}{
		{
			description: "Create a NotificationStatus with several fields",
			generation:  42,
			commit:      "abc",
			eventSequence: controller.NotificationEventSequence{
				Delivered: []controller.NotificationDelivery{
					{
						Trigger: "trigger",
						Destination: services.Destination{
							Service:   "service",
							Recipient: "recipient",
						},
						AlreadyNotified: true,
					},
					{
						Trigger: "trigger2",
						Destination: services.Destination{
							Service:   "service2",
							Recipient: "recipient2",
						},
						AlreadyNotified: false,
					},
				},
				Errors: []error{
					fmt.Errorf("this is an error"),
					fmt.Errorf("this is another error"),
				},
				Warnings: []error{
					fmt.Errorf("this is a warning"),
					fmt.Errorf("this is another warning"),
				},
			},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
		},
		{
			description: "Create a NotificationStatus with each field set",
			generation:  42,
			commit:      "abc",
			eventSequence: controller.NotificationEventSequence{
				Delivered: []controller.NotificationDelivery{
					{
						Trigger: "trigger",
						Destination: services.Destination{
							Service:   "service",
							Recipient: "recipient",
						},
						AlreadyNotified: true,
					},
				},
				Errors: []error{
					fmt.Errorf("this is an error"),
				},
				Warnings: []error{
					fmt.Errorf("this is a warning"),
				},
			},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
				},
			},
		},
		{
			description: "Create a NotificationStatus with no warnings",
			generation:  42,
			commit:      "abc",
			eventSequence: controller.NotificationEventSequence{
				Delivered: []controller.NotificationDelivery{
					{
						Trigger: "trigger",
						Destination: services.Destination{
							Service:   "service",
							Recipient: "recipient",
						},
						AlreadyNotified: true,
					},
				},
				Errors: []error{
					fmt.Errorf("this is an error"),
				},
			},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
				},
			},
		},
		{
			description: "Create a NotificationStatus with no errors",
			generation:  42,
			commit:      "abc",
			eventSequence: controller.NotificationEventSequence{
				Delivered: []controller.NotificationDelivery{
					{
						Trigger: "trigger",
						Destination: services.Destination{
							Service:   "service",
							Recipient: "recipient",
						},
						AlreadyNotified: true,
					},
				},
			},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
				},
			},
		},
		{
			description: "Create a NotificationStatus with no deliveries",
			generation:  42,
			commit:      "abc",
			eventSequence: controller.NotificationEventSequence{
				Errors: []error{
					fmt.Errorf("this is an error"),
				},
			},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
				},
			},
		},
		{
			description:   "Create a NotificationStatus with no errors or deliveries",
			generation:    42,
			commit:        "abc",
			eventSequence: controller.NotificationEventSequence{},
			expectedStatus: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			status := CreateNotificationStatus(tc.generation, tc.commit, tc.eventSequence)
			if tc.expectedStatus.ObservedGeneration != status.ObservedGeneration {
				t.Errorf("expected generation %d, got %d", tc.expectedStatus.ObservedGeneration, status.ObservedGeneration)
			}
			if tc.expectedStatus.Commit != status.Commit {
				t.Errorf("expected commit %s, got %s", tc.expectedStatus.Commit, status.Commit)
			}
			if !reflect.DeepEqual(tc.expectedStatus.Deliveries, status.Deliveries) {
				t.Errorf("expected deliveries %v, got %v", tc.expectedStatus.Deliveries, status.Deliveries)
			}
			if !reflect.DeepEqual(tc.expectedStatus.Errors, status.Errors) {
				t.Errorf("expected errors %v, got %v", tc.expectedStatus.Errors, status.Errors)
			}
		})
	}
}

func TestIsNotificationStatusSame(t *testing.T) {
	testCases := []struct {
		description  string
		left         v1beta1.NotificationStatus
		right        v1beta1.NotificationStatus
		expectedSame bool
	}{
		{
			description: "Equal NotificationStatus objects with multiple fields",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
			expectedSame: true,
		},
		{
			description: "Non-equal NotificationStatus objects with different ObservedGeneration",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 2112,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
			expectedSame: false,
		},
		{
			description: "Non-Equal NotificationStatus objects with different Commit",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "def",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
			},
			expectedSame: false,
		},
		{
			description: "Non-Equal NotificationStatus objects with different Deliveries",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
			},
			expectedSame: false,
		},
		{
			description: "Non-Equal NotificationStatus objects with different Errors",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
				},
			},
			expectedSame: false,
		},
		{
			description: "Non-Equal NotificationStatus objects with different Warnings",
			left: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
					{
						ErrorMessage: "this is another warning",
					},
				},
			},
			right: v1beta1.NotificationStatus{
				ObservedGeneration: 42,
				Commit:             "abc",
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "trigger",
						Service:         "service",
						Recipient:       "recipient",
						AlreadyNotified: true,
					},
					{
						Trigger:         "trigger2",
						Service:         "service2",
						Recipient:       "recipient2",
						AlreadyNotified: false,
					},
				},
				Errors: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is an error",
					},
					{
						ErrorMessage: "this is another error",
					},
				},
				Warnings: []v1beta1.NotificationError{
					{
						ErrorMessage: "this is a warning",
					},
				},
			},
			expectedSame: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			same := IsNotificationStatusSame(tc.left, tc.right)
			if tc.expectedSame != same {
				t.Errorf("expected same to be %v, got %v", tc.expectedSame, same)
			}
		})
	}
}
