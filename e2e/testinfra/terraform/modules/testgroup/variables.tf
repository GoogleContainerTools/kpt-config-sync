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

variable "channel" {
  type = string
  description = "The cluster release channel. One of RAPID, REGULAR, STABLE, UNSPECIFIED"
  default = "UNSPECIFIED"
}

variable "min_master_version" {
  type = string
  description = "Pin to this master version for the cluster"
  default = null
}

variable "subnetwork" {
  type = string
  default = "e2e-subnetwork-1"
}

variable "network" {
  type = string
  default = "prow-e2e-network"
}

variable "prefix" {
  type = string
  description = "Prefix to use for test group name"
  default = "multi-repo"
}

variable "suffix" {
  type = string
  description = "Suffix to use for test group name"
  default = null
}

variable "num_clusters" {
  type = number
  description = "Number of test group clusters to create"
  default = 8
}

locals {
  suffix = var.suffix != null ? "standard-${var.channel}-${var.suffix}" : (var.min_master_version != null ? "standard-${var.channel}-${var.min_master_version}" : "standard-${var.channel}")
}

