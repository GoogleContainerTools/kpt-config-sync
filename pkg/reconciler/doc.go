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

// Package reconciler declares the reconciler process which is described in
// go/config-sync-multi-repo. This process has four main components:
// git-sync, parser, applier, remediator
//
// git-sync is a third party component that runs in a sidecar container. The
// other three components are all declared in their respective packages.
package reconciler
