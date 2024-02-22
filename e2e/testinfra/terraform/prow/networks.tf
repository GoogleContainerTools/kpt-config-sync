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

resource "google_compute_network" "e2e-network" {
  name                    = "prow-e2e-network-1"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-1" {
  name          = "prow-e2e-subnetwork-1"
  ip_cidr_range = "10.0.0.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-2" {
  name          = "prow-e2e-subnetwork-2"
  ip_cidr_range = "10.0.16.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-3" {
  name          = "prow-e2e-subnetwork-3"
  ip_cidr_range = "10.0.32.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-4" {
  name          = "prow-e2e-subnetwork-4"
  ip_cidr_range = "10.0.48.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-5" {
  name          = "prow-e2e-subnetwork-5"
  ip_cidr_range = "10.0.64.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-6" {
  name          = "prow-e2e-subnetwork-6"
  ip_cidr_range = "10.0.80.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-7" {
  name          = "prow-e2e-subnetwork-7"
  ip_cidr_range = "10.0.96.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-8" {
  name          = "prow-e2e-subnetwork-8"
  ip_cidr_range = "10.0.112.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-9" {
  name          = "prow-e2e-subnetwork-9"
  ip_cidr_range = "10.0.128.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-10" {
  name          = "prow-e2e-subnetwork-10"
  ip_cidr_range = "10.0.144.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-11" {
  name          = "prow-e2e-subnetwork-11"
  ip_cidr_range = "10.0.160.0/20"
  region        = "us-central1"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-1" {
  name          = "prow-e2e-subnetwork-usc2-1"
  ip_cidr_range = "10.1.0.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-2" {
  name          = "prow-e2e-subnetwork-usc2-2"
  ip_cidr_range = "10.1.16.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-3" {
  name          = "prow-e2e-subnetwork-usc2-3"
  ip_cidr_range = "10.1.32.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-4" {
  name          = "prow-e2e-subnetwork-usc2-4"
  ip_cidr_range = "10.1.48.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-5" {
  name          = "prow-e2e-subnetwork-usc2-5"
  ip_cidr_range = "10.1.64.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-6" {
  name          = "prow-e2e-subnetwork-usc2-6"
  ip_cidr_range = "10.1.80.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-7" {
  name          = "prow-e2e-subnetwork-usc2-7"
  ip_cidr_range = "10.1.96.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-8" {
  name          = "prow-e2e-subnetwork-usc2-8"
  ip_cidr_range = "10.1.112.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-9" {
  name          = "prow-e2e-subnetwork-usc2-9"
  ip_cidr_range = "10.1.128.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-10" {
  name          = "prow-e2e-subnetwork-usc2-10"
  ip_cidr_range = "10.1.144.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}

resource "google_compute_subnetwork" "e2e-subnetwork-usc2-11" {
  name          = "prow-e2e-subnetwork-usc2-11"
  ip_cidr_range = "10.1.160.0/20"
  region        = "us-central2"
  network       = google_compute_network.e2e-network.id
  description = "Subnetwork for use in e2e test clusters"
  private_ip_google_access = false
}