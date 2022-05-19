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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/webhook/configuration"
)

// WaitForWebhookReadiness waits up to 3 minutes for the wehbook becomes ready.
// If the webhook still is not ready after 3 minutes, the test would fail.
func WaitForWebhookReadiness(nt *NT) {
	nt.T.Logf("Waiting for the webhook to become ready: %v", time.Now())
	_, err := Retry(3*time.Minute, func() error {
		return webhookReadiness(nt)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Logf("The webhook is ready at %v", time.Now())
}

func webhookReadiness(nt *NT) error {
	err := nt.Validate(configuration.ShortName, configmanagement.ControllerNamespace,
		&appsv1.Deployment{}, isAvailableDeployment)
	if err != nil {
		return fmt.Errorf("webhook server is not ready: %w", err)
	}
	return nil
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

	_, err = Retry(30*time.Second, func() error {
		return nt.Validate(webhookName, "", &admissionv1.ValidatingWebhookConfiguration{},
			HasAnnotation(metadata.WebhookconfigurationKey, metadata.WebhookConfigurationUpdateDisabled))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Kubectl("delete", webhookGK, webhookName)
	if err != nil {
		nt.T.Fatalf("got `kubectl delete %s %s` error %v %s, want return nil", webhookGK, webhookName, err, out)
	}

	_, err = Retry(30*time.Second, func() error {
		return nt.ValidateNotFound(webhookName, "", &admissionv1.ValidatingWebhookConfiguration{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func installWebhook(nt *NT, nomos ntopts.Nomos) {
	nt.T.Helper()
	objs := parseManifests(nt, nomos)
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil || labels["app"] != "admission-webhook" {
			continue
		}
		nt.T.Logf("installWebhook obj: %v", core.GKNN(o))
		err := nt.Create(o)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			nt.T.Fatal(err)
		}
	}
}
