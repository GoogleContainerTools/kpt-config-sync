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

type ConfigMapMutator func(map[string]string)
type SecretMutator func(map[string][]byte)

const notificationUsernameSecretKey = "username"
const notificationPasswordSecretKey = "password"

func WithNotificationUsername(username string) SecretMutator {
	return func(secretData map[string][]byte) {
		secretData[notificationUsernameSecretKey] = []byte(username)
	}
}

func WithNotificationPassword(password string) SecretMutator {
	return func(secretData map[string][]byte) {
		secretData[notificationPasswordSecretKey] = []byte(password)
	}
}

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

func WithOnSyncSyncedTrigger(cmData map[string]string) {
	cmData["trigger.on-sync-synced"] = `- when: any(sync.status.conditions, {.commit != nil && .type == 'Syncing' && .status == 'False' && .message == 'Sync Completed' && .errorSourceRefs == nil && .errors == nil})
  oncePer: sync.status.lastSyncedCommit
  send: [sync-synced]`
}

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
