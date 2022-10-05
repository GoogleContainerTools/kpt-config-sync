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

# Multi repo standard clusters
module "multi-repo-stable" {
  source = "../modules/testgroup"
  channel = "stable"
}

module "multi-repo-regular" {
  source = "../modules/testgroup"
  channel = "regular"
}

module "multi-repo-rapid" {
  source = "../modules/testgroup"
  channel = "rapid"
}

module "multi-repo-rapid-latest" {
  source = "../modules/testgroup"
  channel = "rapid"
  min_master_version = "latest"
}

module "multi-repo-psp" {
  source = "../modules/testgroup"
  channel = "regular"
  suffix = "psp"
}

module "multi-repo-bitbucket" {
  source = "../modules/testgroup"
  channel = "regular"
  suffix = "bitbucket"
}

module "multi-repo-gitlab" {
  source = "../modules/testgroup"
  channel = "regular"
  suffix = "gitlab"
}

# Multi repo autopilot clusters
module "multi-repo-autopilot-stable" {
  source = "../modules/testgroup_autopilot"
  channel = "stable"
}

module "multi-repo-autopilot-regular" {
  source = "../modules/testgroup_autopilot"
  channel = "regular"
}

module "multi-repo-autopilot-rapid" {
  source = "../modules/testgroup_autopilot"
  channel = "rapid"
}

module "multi-repo-autopilot-rapid-latest" {
  source = "../modules/testgroup_autopilot"
  channel = "rapid"
  min_master_version = "latest"
}

# Mono repo clusters
module "mono-repo-stable" {
  source = "../modules/testgroup"
  prefix = "mono-repo"
  channel = "stable"
  num_clusters = 3
}

module "mono-repo-regular" {
  source = "../modules/testgroup"
  prefix = "mono-repo"
  channel = "regular"
  num_clusters = 3
}

module "mono-repo-rapid" {
  source = "../modules/testgroup"
  prefix = "mono-repo"
  channel = "rapid"
  num_clusters = 3
}

module "mono-repo-rapid-latest" {
  source = "../modules/testgroup"
  prefix = "mono-repo"
  channel = "rapid"
  min_master_version = "latest"
  num_clusters = 3
}

# Release branch clusters
# These will need to be switched to test groups for v1.14
module "mono-repo-release-regular" {
  source = "../modules/e2ecluster"
  name = "release-mono-repo-regular"
  channel = "regular"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-release-stable" {
  source = "../modules/e2ecluster"
  name = "release-multi-repo-stable"
  channel = "stable"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-release-regular" {
  source = "../modules/e2ecluster"
  name = "release-multi-repo-regular"
  channel = "regular"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-release-rapid" {
  source = "../modules/e2ecluster"
  name = "release-multi-repo-rapid"
  channel = "rapid"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-release-rapid-latest" {
  source = "../modules/e2ecluster"
  name = "release-multi-repo-rapid-latest"
  channel = "rapid"
  min_master_version = "latest"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

# One off clusters
module "multi-repo-kind" {
  source = "../modules/e2ecluster"
  name = "multi-repo-kind"
  channel = "regular"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "mono-repo-kind" {
  source = "../modules/e2ecluster"
  name = "mono-repo-kind"
  channel = "regular"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-kcc" {
  source = "../modules/e2ecluster"
  name = "multi-repo-kcc"
  channel = "regular"
  enable_config_connector = true
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "multi-repo-gcenode" {
  source = "../modules/e2ecluster"
  name = "multi-repo-gcenode"
  channel = "regular"
  enable_workload_identity = false
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

module "stress-test" {
  source = "../modules/e2ecluster"
  name = "stress-test"
  channel = "regular"
  subnetwork = google_compute_subnetwork.e2e-subnetwork-1.name
  network = google_compute_network.e2e-network.name
}

