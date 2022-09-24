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
  provider = google-beta
  project = data.google_project.project.project_id
  name               = var.name
  ip_allocation_policy {
    cluster_ipv4_cidr_block = "/19"
  }
  location           = "us-central1-a"
  network = var.network
  subnetwork = var.subnetwork
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
  workload_identity_config {
    workload_pool = var.enable_workload_identity ? "${data.google_project.project.project_id}.svc.id.goog" : null
  }
  addons_config {
    config_connector_config {
      enabled = var.enable_config_connector
    }
  }
  timeouts {
    create = "30m"
    update = "40m"
  }
  # Remove default node pool
  remove_default_node_pool = true
  initial_node_count = 1
}

resource "google_container_node_pool" "pool" {
  name = "pool"
  node_count = var.node_count
  cluster = google_container_cluster.e2e.name
  location = "us-central1-a"
  node_config {
    machine_type = var.machine_type
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]
  }
}
