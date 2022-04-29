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

package applier

/*
Package applier is a component of a reconcile process (RP),
designed in the Config Sync V2 Multi-Repo design.

Initialization
- When Initialzing a new applier, the `NewRootApplier` shall be used for root RP and the
`NewNamespaceApplier` shall be used for namespace RP.


Running Workflow
- Periodic run: The applier runs periodically every hour through a Refresh() function.
The running frequency is configurable via a "resyncPeriod" argument. This periodic run is
designed to make sure the resource states in the API server are in sync with the real state
in the (cached) git repo. The git repo resource is cached in applier.
      ctx := context.Background()
      resyncPeriod := time.Duration(1) * time.Hour
      stopCh := make(chan struct{})
      a = NewRootApplier(ctx, reader, baseApplier)
      a.Run(ctx, resyncPeriod, stopCh)

- Run when the git resource changes: The applier is forced to run once when a git resource
change is detected. The parser will call the Apply() function and provide the latest parsed
git resource. This git resource will be cached in the applier.
      ctx := context.Background()
      a = NewRootApplier(ctx, reader, baseApplier)
      a.Apply(ctx, newDeclaredFileObjects)
*/
