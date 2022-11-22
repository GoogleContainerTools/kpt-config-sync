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
	"reflect"

	"github.com/argoproj/notifications-engine/pkg/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// CreateNotificationStatus returns a Config Sync NotificationStatus with the provided
// NotificationEventSequence from the notification controller.
func CreateNotificationStatus(generation int64, commit string, eventSequence controller.NotificationEventSequence) v1beta1.NotificationStatus {
	notificationStatus := v1beta1.NotificationStatus{
		ObservedGeneration: generation,
		Commit:             commit,
		LastUpdate:         metav1.Now(),
	}
	for _, e := range eventSequence.Errors {
		notificationStatus.Errors = append(notificationStatus.Errors, v1beta1.NotificationError{
			ErrorMessage: e.Error(),
		})
	}
	for _, w := range eventSequence.Warnings {
		notificationStatus.Warnings = append(notificationStatus.Warnings, v1beta1.NotificationError{
			ErrorMessage: w.Error(),
		})
	}
	for _, d := range eventSequence.Delivered {
		notificationStatus.Deliveries = append(notificationStatus.Deliveries, v1beta1.NotificationDelivery{
			Trigger:         d.Trigger,
			Service:         d.Destination.Service,
			Recipient:       d.Destination.Recipient,
			AlreadyNotified: d.AlreadyNotified,
		})
	}
	return notificationStatus
}

// IsNotificationStatusSame returns whether the two provided NotificationStatus
// objects are equal. Ignores certain fields such as timestamps.
func IsNotificationStatusSame(left, right v1beta1.NotificationStatus) bool {
	if left.Commit != right.Commit {
		return false
	}
	if left.ObservedGeneration != right.ObservedGeneration {
		return false
	}
	if !reflect.DeepEqual(left.Errors, right.Errors) {
		return false
	}
	if !reflect.DeepEqual(left.Warnings, right.Warnings) {
		return false
	}
	if !reflect.DeepEqual(left.Deliveries, right.Deliveries) {
		return false
	}
	return true
}
