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

package watch

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type action struct {
	event watch.EventType
	obj   client.Object
}

func TestFilteredWatcher(t *testing.T) {
	scope := declared.Scope("test")
	syncName := "rs"

	deployment1 := fake.DeploymentObject(core.Name("hello"))
	deployment1Beta := fake.DeploymentObject(core.Name("hello"))
	deployment1Beta.GetObjectKind().SetGroupVersionKind(deployment1Beta.GroupVersionKind().GroupKind().WithVersion("beta1"))

	deployment2 := fake.DeploymentObject(core.Name("world"))
	deployment3 := fake.DeploymentObject(core.Name("nomes"))

	managedBySelfDeployment := fake.DeploymentObject(core.Name("not-declared"),
		syncertest.ManagementEnabled, difftest.ManagedBy(scope, syncName))
	managedByOtherDeployment := fake.DeploymentObject(core.Name("not-declared"),
		syncertest.ManagementEnabled, difftest.ManagedBy("other", "other-rs"))
	deploymentForRoot := fake.DeploymentObject(core.Name("managed-by-root"), difftest.ManagedBy(declared.RootReconciler, "any-rs"))

	testCases := []struct {
		name     string
		declared []client.Object
		actions  []action
		want     []core.ID
	}{
		{
			"Enqueue events for declared resources",
			[]client.Object{
				deployment1,
				deployment2,
				deployment3,
			},
			[]action{
				{
					watch.Added,
					deployment1,
				},
				{
					watch.Modified,
					deployment2,
				},
				{
					watch.Deleted,
					deployment3,
				},
			},
			[]core.ID{
				core.IDOf(deployment1),
				core.IDOf(deployment2),
				core.IDOf(deployment3),
			},
		},
		{
			"Filter events for undeclared-but-managed-by-other-reconciler resource",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Modified,
					managedByOtherDeployment,
				},
			},
			nil,
		},
		{
			"Enqueue events for undeclared-but-managed-by-this-reconciler resource",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Modified,
					managedBySelfDeployment,
				},
			},
			[]core.ID{
				core.IDOf(managedBySelfDeployment),
			},
		},
		{
			"Filter events for undeclared-and-unmanaged resources",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Added,
					deployment2,
				},
				{
					watch.Added,
					deployment3,
				},
			},
			nil,
		},
		{
			"Filter events for declared resource with different manager",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Modified,
					deploymentForRoot,
				},
			},
			nil,
		},
		{
			"Filter events for declared resource with different GVK",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Modified,
					deployment1Beta,
				},
			},
			nil,
		},
		{
			"Handle bookmark events",
			[]client.Object{
				deployment1,
			},
			[]action{
				{
					watch.Modified,
					deployment1,
				},
				{
					watch.Bookmark,
					deployment1,
				},
			},
			[]core.ID{
				core.IDOf(deployment1),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dr := &declared.Resources{}
			ctx := context.Background()
			if _, err := dr.Update(ctx, tc.declared); err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			base := watch.NewFake()
			q := queue.New("test")
			cfg := watcherConfig{
				scope:     scope,
				syncName:  syncName,
				resources: dr,
				queue:     q,
				startWatch: func(options metav1.ListOptions) (watch.Interface, error) {
					return base, nil
				},
			}
			w := NewFiltered(ctx, cfg)

			go func() {
				for _, a := range tc.actions {
					// Each base.Action() blocks until the code within w.Run() reads its
					// event from the queue.
					base.Action(a.event, a.obj)
				}
				// This is not reached until after w.Run() reads the last event from the
				// queue.
				w.Stop()
			}()
			// w.Run() blocks until w.Stop() is called.
			if err := w.Run(ctx); err != nil {
				t.Fatalf("got Run() = %v, want Run() = <nil>", err)
			}

			var got []core.ID
			for q.Len() > 0 {
				obj, shutdown := q.Get()
				if shutdown {
					t.Fatal("Object queue was shut down unexpectedly.")
				}
				got = append(got, core.IDOf(obj))
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("did not get desired object IDs: %v", diff)
			}
		})
	}
}
