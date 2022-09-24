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

data "google_project" "project" {
}

resource "google_container_cluster" "e2e" {
  name               = var.name
  ip_allocation_policy {
    cluster_ipv4_cidr_block = "/19"
  }
  location           = "us-central1"
  network = var.network
  subnetwork = var.subnetwork
  enable_autopilot = true
  release_channel {
    channel = upper(var.channel)
  }
  # https://cloud.google.com/kubernetes-engine/versioning#specifying_cluster_version
  min_master_version = var.min_master_version
  master_auth {
    client_certificate_config {
      issue_client_certificate = false
    }
  }
  timeouts {
    create = "30m"
    update = "40m"
  }
}
