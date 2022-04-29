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

import "kpt.dev/configsync/pkg/api/configsync"

// ShortName is the short name of the ValidatingWebhookConfiguration for the
// Admission Controller.
const ShortName = "admission-webhook"

// Name is both:
// 1) The metadata.name of the ValidatingWebhookConfiguration, and
// 2) The .name of every ValidatingWebhook in the ValidatingWebhookConfiguration.
const Name = ShortName + "." + configsync.GroupName

// ServingPath is the path the webhook is served.
const ServingPath = "/" + ShortName

// ServicePort matches the service port in the admission-webhook Service object.
// Use 443 here to be consistent with the settings of other webhooks in ACM.
const ServicePort = 443

// ContainerPort is the port where the webhook serves at.
//
// To communicate with a webhook, the API Server sends requests directly to the webhook pod(s)
// (i.e., the target port ofof the webhook Service) instead of the service port of the Service.
//
// By default, the firewall rules on a private GKE cluster restrict your cluster control plane to
// only initiate TCP connections to your nodes and Pods on ports 443 (HTTPS) and 10250 (kubelet).
// See https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters#add_firewall_rules.
//
// Setting ContainerPort to a value other than 443 or 10250 would require our customers to add a firewall
// rule allowing the API Server to initiate TCP connections to the webhook Pods on the port.
//
// Setting ContainerPort to 443 requires elevated permissions, and should be avoided.
const ContainerPort = 10250

// CertDir matches the mountPath specified in admission-webhook.yaml.
const CertDir = "/certs"
