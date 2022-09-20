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

# Standard dev clusters
module "dev-stable" {
  source = "../modules/testgroup"
  prefix = "dev"
  channel = "stable"
  num_clusters = var.num_clusters
}

module "dev-regular" {
  source = "../modules/testgroup"
  prefix = "dev"
  channel = "regular"
  num_clusters = var.num_clusters
}

module "dev-rapid" {
  source = "../modules/testgroup"
  prefix = "dev"
  channel = "rapid"
  num_clusters = var.num_clusters
}

module "dev-rapid-latest" {
  source = "../modules/testgroup"
  prefix = "dev"
  channel = "rapid"
  min_master_version = "latest"
  num_clusters = var.num_clusters
}

# Autopilot dev clusters
module "dev-autopilot-stable" {
  source = "../modules/testgroup_autopilot"
  prefix = "dev"
  channel = "stable"
  num_clusters = var.num_clusters
}

module "dev-autopilot-regular" {
  source = "../modules/testgroup_autopilot"
  prefix = "dev"
  channel = "regular"
  num_clusters = var.num_clusters
}

module "dev-autopilot-rapid" {
  source = "../modules/testgroup_autopilot"
  prefix = "dev"
  channel = "rapid"
  num_clusters = var.num_clusters
}

module "dev-autopilot-rapid-latest" {
  source = "../modules/testgroup_autopilot"
  prefix = "dev"
  channel = "rapid"
  min_master_version = "latest"
  num_clusters = var.num_clusters
}
