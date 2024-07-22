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

package nomostest

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/webhook/configuration"
)

// WaitForWebhookReadiness waits up to 3 minutes for the wehbook becomes ready.
// If the webhook still is not ready after 3 minutes, the test would fail.
func WaitForWebhookReadiness(nt *NT) {
	err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
		configuration.ShortName, configmanagement.ControllerNamespace,
		testwatcher.WatchTimeout(3*time.Minute))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// StopWebhook removes the Config Sync ValidatingWebhookConfiguration object.
func StopWebhook(nt *NT) {
	if *nt.WebhookDisabled {
		return
	}
	webhookName := configuration.Name
	err := nt.KubeClient.Delete(k8sobjects.AdmissionWebhookObject(webhookName))
	if err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		nt.T.Fatalf("got `kubectl delete %s` error %v, want return nil", webhookName, err)
	}
	err = nt.Watcher.WatchForNotFound(kinds.ValidatingWebhookConfiguration(),
		webhookName, "",
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	*nt.WebhookDisabled = true
}

// InstallWebhook installs the Config Sync ValidatingWebhookConfiguration object.
func InstallWebhook(nt *NT) error {
	objs, err := parseConfigSyncManifests(nt)
	if err != nil {
		return err
	}
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil || labels["app"] != "admission-webhook" {
			continue
		}
		nt.T.Logf("InstallWebhook obj: %v", core.GKNN(o))
		if err := nt.KubeClient.Apply(o); err != nil {
			return err
		}
	}
	*nt.WebhookDisabled = false
	return nil
}
