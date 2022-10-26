/**
 * Copyright 2022 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

resource "google_artifact_registry_repository" "ar-public" {
  location      = "us"
  repository_id = "config-sync-test-public"
  description   = "A public AR repository used for Config Sync e2e tests"
  format        = "DOCKER"
}

# Grant public access to this registry by granting reader to allUsers.
# Note this will fail if the project enforces Domain Restricted Sharing. See:
# https://cloud.google.com/resource-manager/docs/organization-policy/restricting-domains
# TODO: uncomment this once prow project allows granting allUsers
//resource "google_artifact_registry_repository_iam_member" "member" {
//  project = google_artifact_registry_repository.ar-public.project
//  location = google_artifact_registry_repository.ar-public.location
//  repository = google_artifact_registry_repository.ar-public.name
//  role = "roles/artifactregistry.reader"
//  member = "allUsers"
//}
