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

package configuration

import (
	"sort"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// Merge merges two sets of ValidatingWebhookConfigurations so that the
// resulting Configuration matches the union of the initial two.
//
// The intent is that left is the Configuration currently on the server and
// right is the Configuration generated from the declared configuration in a
// repository.
// (The logic should be symmetric, so this shouldn't have to be the case.)
//
// The resulting merged Configuration meets the following criteria:
// 1) All Webhooks contain exactly one rule, matching all resources of a given
//      GroupVersion.
// 2) Webhooks are sorted by the GroupVersion they match.
// 3) All invalid webhooks are removed.
//
// Cannot return error or panic as we never want this to get stuck.
//
// Modifies left.
func Merge(left, right *admissionv1.ValidatingWebhookConfiguration) *admissionv1.ValidatingWebhookConfiguration {
	webhooksMap := make(map[schema.GroupVersion]admissionv1.ValidatingWebhook)
	for _, webhook := range append(left.Webhooks, right.Webhooks...) {
		// Rules match a single API Group.
		if len(webhook.Rules) == 0 ||
			len(webhook.Rules[0].APIGroups) == 0 {
			// Invalid ValidatingWebhook, discard.
			// We do NOT want to fail here as the Configuration on the API Server
			// may have been changed by a user. We don't want to put ourselves in an
			// infinite error loop.
			klog.Warning(InvalidWebhookWarning("removed admission webhook specifying no API Groups"))
			continue
		}
		group := webhook.Rules[0].APIGroups[0]

		// Rules are for objects declared in a specific API Version. We read this
		// from the ObjectSelector.
		if webhook.ObjectSelector == nil || webhook.ObjectSelector.MatchLabels == nil {
			// The webhook is configured to match objects in a way we don't support, so
			// ignore it.
			klog.Warning(InvalidWebhookWarning("removed admission webhook missing objectSelector.matchLabels"))
			continue
		}
		version := webhook.ObjectSelector.MatchLabels[metadata.DeclaredVersionLabel]

		if group == "*" || version == "*" {
			// This was probably added by a user. It can cause the webhook to have
			// unexpected effects, so we ignore these rules when merging.
			klog.Warning(InvalidWebhookWarning("removed admission webhook matching wildcard group or version"))
			continue
		}

		gv := schema.GroupVersion{Group: group, Version: version}
		if _, found := webhooksMap[gv]; !found {
			webhooksMap[gv] = toWebhook(gv)
		}
	}

	var webhooks []admissionv1.ValidatingWebhook
	for _, webhook := range webhooksMap {
		webhooks = append(webhooks, webhook)
	}
	sort.Slice(webhooks, func(i, j int) bool {
		groupI := webhooks[i].Rules[0].APIGroups[0]
		groupJ := webhooks[j].Rules[0].APIGroups[0]
		if groupI != groupJ {
			return groupI < groupJ
		}
		versionI := webhooks[i].ObjectSelector.MatchLabels[metadata.DeclaredVersionLabel]
		versionJ := webhooks[j].ObjectSelector.MatchLabels[metadata.DeclaredVersionLabel]
		return versionI < versionJ
	})

	left.Webhooks = webhooks
	return left
}

// InvalidWebhookWarningCode signals that the webhook was illegally modified.
// We automatically resolve these issues. There's no point in breaking ourselves
// when we encounter these issues so we immediately fix these.
const InvalidWebhookWarningCode = "2014"

var invalidWebhookWarning = status.NewErrorBuilder(InvalidWebhookWarningCode)

// InvalidWebhookWarning lets the user know we removed an invalid webhook when
// merging.
func InvalidWebhookWarning(msg string) status.Error {
	return invalidWebhookWarning.Sprint(msg).Build()
}
