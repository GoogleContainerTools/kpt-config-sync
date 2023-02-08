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
	"fmt"
	"time"

	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/webhook/configuration"
)

// WaitForWebhookReadiness waits up to 3 minutes for the wehbook becomes ready.
// If the webhook still is not ready after 3 minutes, the test would fail.
func WaitForWebhookReadiness(nt *NT) {
	err := WatchForCurrentStatus(nt, kinds.Deployment(),
		configuration.ShortName, configmanagement.ControllerNamespace,
		WatchTimeout(3*time.Minute))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// StopWebhook removes the Config Sync ValidatingWebhookConfiguration object.
func StopWebhook(nt *NT) {
	webhookName := configuration.Name
	webhookGK := "validatingwebhookconfigurations.admissionregistration.k8s.io"

	out, err := nt.Kubectl("annotate", webhookGK, webhookName, fmt.Sprintf("%s=%s", metadata.WebhookconfigurationKey, metadata.WebhookConfigurationUpdateDisabled))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate %s %s %s=%s` error %v %s, want return nil",
			webhookGK, webhookName, metadata.WebhookconfigurationKey, metadata.WebhookConfigurationUpdateDisabled, err, out)
	}

	err = WatchObject(nt, kinds.ValidatingWebhookConfiguration(),
		webhookName, "",
		[]Predicate{
			HasAnnotation(metadata.WebhookconfigurationKey, metadata.WebhookConfigurationUpdateDisabled),
		},
		WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Kubectl("delete", webhookGK, webhookName)
	if err != nil {
		nt.T.Fatalf("got `kubectl delete %s %s` error %v %s, want return nil", webhookGK, webhookName, err, out)
	}

	err = WatchForNotFound(nt, kinds.ValidatingWebhookConfiguration(),
		webhookName, "",
		WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}
	*nt.WebhookDisabled = true
}

func installWebhook(nt *NT) error {
	objs, err := parseManifests(nt)
	if err != nil {
		return err
	}
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil || labels["app"] != "admission-webhook" {
			continue
		}
		nt.T.Logf("installWebhook obj: %v", core.GKNN(o))
		if err := nt.Apply(o); err != nil {
			return err
		}
	}
	*nt.WebhookDisabled = false
	return nil
}
