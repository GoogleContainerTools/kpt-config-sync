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

package nomostest

import "fmt"

// ConfigMapMutator is a function which modifies the Data in a ConfigMap
type ConfigMapMutator func(map[string]string)

// SecretMutator is a function which modifies the Data in a Secret
type SecretMutator func(map[string][]byte)

const notificationUsernameSecretKey = "username"
const notificationPasswordSecretKey = "password"

// WithNotificationUsername sets the value of the username field in the notification Secret
func WithNotificationUsername(username string) SecretMutator {
	return func(secretData map[string][]byte) {
		secretData[notificationUsernameSecretKey] = []byte(username)
	}
}

// WithNotificationPassword sets the value of the password field in the notification Secret
func WithNotificationPassword(password string) SecretMutator {
	return func(secretData map[string][]byte) {
		secretData[notificationPasswordSecretKey] = []byte(password)
	}
}

// WithLocalWebhookService adds the local webhook service to the ConfigMap
func WithLocalWebhookService(cmData map[string]string) {
	cmData["service.webhook.local"] = fmt.Sprintf(
		`url: http://%s.%s:%d
headers: #optional headers
- name: Content-Type
  value: application/json
basicAuth:
  username: $%s
  password: $%s`,
		testNotificationWebhookServer,
		testNotificationWebhookNamespace,
		TestNotificationWebhookPort,
		notificationUsernameSecretKey,
		notificationPasswordSecretKey,
	)
}

// WithOnSyncSyncedTrigger adds the on-sync-synced trigger to the ConfigMap
func WithOnSyncSyncedTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-synced"] = `- when: any(sync.status.conditions, {.commit != nil && .type == 'Syncing' && .status == 'False' && .message == 'Sync Completed' && .errorSourceRefs == nil && .errors == nil})
  oncePer: sync.status.lastSyncedCommit
  send: [sync-synced]`
}

// WithSyncSyncedTemplate adds the sync-synced template to the ConfigMap
func WithSyncSyncedTemplate(cmData map[string]string) {
	cmData["template.sync-synced"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} is synced!"
        }
      }`
}

// WithOnSyncFailedTrigger adds the on-sync-failed trigger to the ConfigMap
// the oncePer unique identifier is the commit concatenated with a hash of all errors
func WithOnSyncFailedTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-failed"] = `- when: any(sync.status.conditions, {.type == 'Syncing' && .status == 'False' && (.errorSourceRefs != nil || .errors != nil)}) 
  oncePer: utils.LastCommit() + '-' + strings.Hash(strings.Join(map(utils.ConfigSyncErrors(), {#.ErrorMessage}), ''))
  send: [sync-failed]`
}

// WithSyncFailedTemplate adds the sync-failed template to the ConfigMap
func WithSyncFailedTemplate(cmData map[string]string) {
	cmData["template.sync-failed"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} failed to sync commit {{call .utils.LastCommit}} on branch {{.sync.spec.git.branch}}!\n\n{{range $index, $e := call .utils.ConfigSyncErrors}}{{js $e.ErrorMessage}}\n\n{{end}}"
        }
      }`
}

// WithOnSyncCreatedTrigger adds the on-sync-created trigger to the ConfigMap
func WithOnSyncCreatedTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-created"] = `- when: true
  oncePer: sync.metadata.name
  send: [sync-created]`
}

// WithSyncCreatedTemplate adds the sync-createed template to the ConfigMap
func WithSyncCreatedTemplate(cmData map[string]string) {
	cmData["template.sync-created"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} created"
        }
      }`
}

// WithOnSyncDeletedTrigger adds the on-sync-deleted trigger to the ConfigMap
func WithOnSyncDeletedTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-deleted"] = `- when: sync.metadata.deletionTimestamp != nil
  oncePer: sync.metadata.name
  send: [sync-deleted]`
}

// WithSyncDeletedTemplate adds the sync-deleted template to the ConfigMap
func WithSyncDeletedTemplate(cmData map[string]string) {
	cmData["template.sync-deleted"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} has been deleted"
        }
      }`
}

// WithOnSyncStalledTrigger adds the on-sync-stalled trigger to the ConfigMap
func WithOnSyncStalledTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-stalled"] = `- when: any(sync.status.conditions, {.type == 'Stalled' && .status == 'True'})
  oncePer: sync.metadata.name
  send: [sync-stalled]`
}

// WithSyncStalledTemplate adds the sync-stalled template to the ConfigMap
func WithSyncStalledTemplate(cmData map[string]string) {
	cmData["template.sync-stalled"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} is stalled"
        }
      }`
}

// WithOnSyncReconcilingTrigger adds the on-sync-reconciling trigger to the ConfigMap
func WithOnSyncReconcilingTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-reconciling"] = `- when: any(sync.status.conditions, {.type == 'Reconciling' && .status == 'True'})
  oncePer: sync.metadata.name
  send: [sync-reconciling]`
}

// WithSyncReconcilingTemplate adds the sync-stalled template to the ConfigMap
func WithSyncReconcilingTemplate(cmData map[string]string) {
	cmData["template.sync-reconciling"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} is currently reconciling. Message: {{ (index .sync.status.conditions 0).message}}, reason: {{ (index .sync.status.conditions 0).reason}}"
        }
      }`
}

// WithOnSyncPendingTrigger adds the on-sync-pending trigger to the ConfigMap
func WithOnSyncPendingTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-pending"] = `- when: any(sync.status.conditions, {.type == 'Syncing' && .status == 'True'})
  oncePer: sync.metadata.name
  send: [sync-pending]`
}

// WithSyncPendingTemplate adds the sync-pending template to the ConfigMap
func WithSyncPendingTemplate(cmData map[string]string) {
	cmData["template.sync-pending"] = `webhook:
  local:
    method: POST
    path: /
    body: |
      {
        "content": {
          "raw": "{{.sync.kind}} {{.sync.metadata.name}} is currently pending. Reason: {{ (index .sync.status.conditions 1).reason}}, message: {{ (index .sync.status.conditions 1).message}}, commit: {{ (index .sync.status.conditions 1).commit}}"
        }
      }`
}
