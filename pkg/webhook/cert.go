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

package webhook

import (
	cert "github.com/open-policy-agent/cert-controller/pkg/rotator"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	secret         = configuration.ShortName + "-cert"
	caName         = "config-sync-ca"
	caOrganization = "config-sync"

	// dnsName is <service name>.<namespace>.svc
	dnsName = configuration.ShortName + "." + configsync.ControllerNamespace + ".svc"
)

// CreateCertsIfNeeded creates all certs for webhooks.
// This function is called from main.go
func CreateCertsIfNeeded(mgr manager.Manager, restartOnSecretRefresh bool) (chan struct{}, error) {
	setupFinished := make(chan struct{})
	err := cert.AddRotator(mgr, &cert.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: configsync.ControllerNamespace,
			Name:      secret,
		},
		CertDir:        configuration.CertDir,
		CAName:         caName,
		CAOrganization: caOrganization,
		DNSName:        dnsName,
		IsReady:        setupFinished,
		Webhooks: []cert.WebhookInfo{{
			Type: cert.Validating,
			Name: configuration.Name,
		}},
		RestartOnSecretRefresh: restartOnSecretRefresh,
	})
	return setupFinished, err
}
