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
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// NotificationAPIGroup is the env var name for api group
	NotificationAPIGroup = "NOTIFICATION_API_GROUP"
	// NotificationAPIVersion is the env var name for api version
	NotificationAPIVersion = "NOTIFICATION_API_VERSION"
	// NotificationAPIKind is the env var name for api kind
	NotificationAPIKind = "NOTIFICATION_API_KIND"
	// NotificationResourceName is the env var name for resource name
	NotificationResourceName = "NOTIFICATION_RESOURCE_NAME"
	// NotificationResourceNamespace is the env var name for resource namespace
	NotificationResourceNamespace = "NOTIFICATION_RESOURCE_NAMESPACE"
	// NotificationResyncPeriod is the env var name for resync period
	NotificationResyncPeriod = "NOTIFICATION_RESYNC_PERIOD"
	// NotificationConfigMapName is the env var name for configmap name
	NotificationConfigMapName = "NOTIFICATION_CONFIGMAP_NAME"
	// NotificationSecretName is the env var name for secret name
	NotificationSecretName = "NOTIFICATION_SECRET_NAME"

	// AnnotationsPrefix is the notification annotations prefix
	AnnotationsPrefix = "notifications." + configsync.GroupName

	// SubscribeAnnotationPrefix is the annotation prefix of the notification subscription.
	SubscribeAnnotationPrefix = AnnotationsPrefix + "/subscribe."

	// MultiSubscriptionsField is the field name for subscriptions configured globally.
	MultiSubscriptionsField = "subscriptions"

	// NotifiedAnnotationPath is the annotation path that indicates it has been notified.
	NotifiedAnnotationPath = ".metadata.annotations.notified." + AnnotationsPrefix
)

// NotificationEnabled returns whether the notification is enabled for the RSync object.
func NotificationEnabled(ctx context.Context, client client.Client, rsNamespace string, annotations map[string]string, notificationConfig *v1beta1.NotificationConfig) (bool, error) {
	hasSubscriptionAnnotation := false
	// subscription via prefix
	for key := range annotations {
		if strings.HasPrefix(key, SubscribeAnnotationPrefix) {
			hasSubscriptionAnnotation = true
		}
	}
	if hasSubscriptionAnnotation && notificationConfig == nil {
		return false, fmt.Errorf("subscription annotation was provided without a notificationConfig")
	}
	if notificationConfig == nil {
		return false, nil
	}
	// ConfigMapRef must be defined
	configMapName := v1beta1.GetConfigMapName(notificationConfig.ConfigMapRef)
	if configMapName == "" && hasSubscriptionAnnotation {
		return false, fmt.Errorf("notificationConfig must have a configMapRef")
	} else if configMapName == "" && !hasSubscriptionAnnotation {
		return false, nil
	}
	// ensure ConfigMap exists
	cm := &corev1.ConfigMap{}
	cmObjectKey := types.NamespacedName{
		Namespace: rsNamespace,
		Name:      configMapName,
	}
	if err := client.Get(ctx, cmObjectKey, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return false, fmt.Errorf("notification ConfigMap %s not found in the %s namespace", cmObjectKey.Name, cmObjectKey.Namespace)
		}
		return false, err
	}
	// subscription via ConfigMap
	_, found := cm.Data[MultiSubscriptionsField]
	if !hasSubscriptionAnnotation && !found {
		return false, nil
	}
	secretName := v1beta1.GetSecretName(notificationConfig.SecretRef)
	if secretName != "" {
		secretObjectKey := types.NamespacedName{
			Name:      secretName,
			Namespace: rsNamespace,
		}
		if err := client.Get(ctx, secretObjectKey, &corev1.Secret{}); err != nil {
			if apierrors.IsNotFound(err) {
				return false, fmt.Errorf("notification Secret %s not found in the %s namespace", secretObjectKey.Name, secretObjectKey.Namespace)
			}
			return false, err
		}
	}
	return true, nil
}
