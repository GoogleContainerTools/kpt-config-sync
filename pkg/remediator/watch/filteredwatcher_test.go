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
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type action struct {
	event     watch.EventType
	obj       runtime.Object
	cancel    bool
	stopRun   bool
	stopWatch bool
}

func TestFilteredWatcher(t *testing.T) {
	scope := declared.Scope("test")
	syncName := "rs"

	deployment1 := fake.DeploymentObject(core.Name("hello"))
	deployment1Beta := fake.DeploymentObjectV1beta1(core.Name("hello"))

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
		watches  [][]action
		timeout  *time.Duration
		want     []core.ID
		wantErr  status.Error
	}{
		{
			name: "Enqueue events for declared resources",
			declared: []client.Object{
				deployment1,
				deployment2,
				deployment3,
			},
			watches: [][]action{{
				{
					event: watch.Added,
					obj:   deployment1,
				},
				{
					event: watch.Modified,
					obj:   deployment2,
				},
				{
					event: watch.Deleted,
					obj:   deployment3,
				},
				{
					stopRun: true,
				},
			}},
			want: []core.ID{
				queue.IDOf(deployment1),
				queue.IDOf(deployment2),
				queue.IDOf(deployment3),
			},
		},
		{
			name: "Filter events for undeclared-but-managed-by-other-reconciler resource",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   managedByOtherDeployment,
				},
				{
					stopRun: true,
				},
			}},
			want: nil,
		},
		{
			name: "Enqueue events for undeclared-but-managed-by-this-reconciler resource",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   managedBySelfDeployment,
				},
				{
					stopRun: true,
				},
			}},
			want: []core.ID{
				queue.IDOf(managedBySelfDeployment),
			},
		},
		{
			name: "Filter events for undeclared-and-unmanaged resources",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Added,
					obj:   deployment2,
				},
				{
					event: watch.Added,
					obj:   deployment3,
				},
				{
					stopRun: true,
				},
			}},
			want: nil,
		},
		{
			name: "Filter events for declared resource with different manager",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   deploymentForRoot,
				},
				{
					stopRun: true,
				},
			}},
			want: nil,
		},
		{
			name: "Filter events for declared resource with different GVK",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   deployment1Beta,
				},
				{
					stopRun: true,
				},
			}},
			want: nil,
		},
		{
			name: "Handle bookmark events",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   deployment1,
				},
				{
					event: watch.Bookmark,
					obj:   deployment1,
				},
				{
					stopRun: true,
				},
			}},
			want: []core.ID{
				queue.IDOf(deployment1),
			},
		},
		{
			name: "Error on context cancellation",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   deployment1,
				},
				{
					cancel: true,
				},
				// Ignored event
				{
					event: watch.Added,
					obj:   deployment2,
				},
				// No Stop
			}},
			want: []core.ID{
				queue.IDOf(deployment1),
			},
			wantErr: status.InternalWrapf(context.Canceled,
				"remediator watch stopped for %s", kinds.Deployment()),
		},
		{
			name:     "Error on context timeout",
			declared: []client.Object{},
			timeout:  pointer.Duration(1 * time.Second),

			watches: [][]action{{
				// No Stop
			}},
			want: nil,
			wantErr: status.InternalWrapf(context.DeadlineExceeded,
				"remediator watch stopped for %s", kinds.Deployment()),
		},
		{
			name: "Error on context cancellation from http client",
			declared: []client.Object{
				deployment1,
			},
			watches: [][]action{{
				{
					event: watch.Modified,
					obj:   deployment1,
				},
				{
					event: watch.Error,
					// Simulate context cancel error from the http client.
					// https://github.com/kubernetes/client-go/blob/v0.26.7/rest/request.go#L785
					// https://github.com/kubernetes/apimachinery/blob/v0.26.7/pkg/watch/streamwatcher.go#L120
					obj: apierrors.NewClientErrorReporter(http.StatusInternalServerError, "GET", string(ClientWatchDecodingCause)).AsObject(
						fmt.Errorf("unable to decode an event from the watch stream: %v", context.Canceled)),
				},
				// Ignored event
				{
					event: watch.Added,
					obj:   deployment2,
				},
				// No Stop
			}},
			want: []core.ID{
				queue.IDOf(deployment1),
			},
			wantErr: status.InternalWrapf(context.Canceled,
				"remediator watch stopped for %s", kinds.Deployment()),
		},
		{
			name: "Retry on context timeout from http client",
			declared: []client.Object{
				deployment1,
				deployment2,
			},
			watches: [][]action{
				{
					{
						event: watch.Modified,
						obj:   deployment1,
					},
					{
						event: watch.Error,
						// Simulate context timeout error from the http client.
						// https://github.com/kubernetes/client-go/blob/v0.26.7/rest/request.go#L785
						// https://github.com/kubernetes/apimachinery/blob/v0.26.7/pkg/watch/streamwatcher.go#L120
						obj: apierrors.NewClientErrorReporter(http.StatusInternalServerError, "GET", string(ClientWatchDecodingCause)).AsObject(
							fmt.Errorf("unable to decode an event from the watch stream: %v", context.DeadlineExceeded)),
					},
				},
				// Error should cause watcher to re-start
				{
					{
						event: watch.Added,
						obj:   deployment2,
					},
					{
						stopRun: true,
					},
				},
			},
			want: []core.ID{
				queue.IDOf(deployment1),
				queue.IDOf(deployment2),
			},
			wantErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dr := &declared.Resources{}
			ctx := context.Background()
			var cancel context.CancelFunc
			if tc.timeout != nil {
				ctx, cancel = context.WithTimeout(ctx, *tc.timeout)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			if _, err := dr.Update(ctx, tc.declared, "unused"); err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			watches := make(chan watch.Interface) // TODO: test startWatch errors
			q := queue.New("test")
			cfg := watcherConfig{
				gvk:       kinds.Deployment(),
				scope:     scope,
				syncName:  syncName,
				resources: dr,
				queue:     q,
				startWatch: func(_ context.Context, options metav1.ListOptions) (watch.Interface, error) {
					return <-watches, nil
				},
				conflictHandler: testfake.NewConflictHandler(),
			}
			w := NewFiltered(cfg)

			go func() {
				for _, actions := range tc.watches {
					// Unblock startWatch with a new fake watcher
					base := watch.NewFake()
					watches <- base
					for _, a := range actions {
						if a.stopWatch {
							base.Stop()
						} else if a.stopRun {
							w.Stop()
						} else if a.cancel {
							cancel()
						} else {
							// Each base.Action() blocks until the code within w.Run() reads its
							// event from the queue.
							base.Action(a.event, a.obj)
						}
					}
				}
			}()
			// w.Run() blocks until w.Stop() is called or the context is cancelled.
			err := w.Run(ctx)
			require.Equal(t, tc.wantErr, err)

			var got []core.ID
			for q.Len() > 0 {
				obj, err := q.Get(context.Background())
				if err != nil {
					t.Fatalf("Object queue was shut down unexpectedly: %v", err)
				}
				got = append(got, queue.IDOf(obj))
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("did not get desired object IDs: %v", diff)
			}
		})
	}
}
