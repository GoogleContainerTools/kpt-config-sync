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

package queue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type action func(t *testing.T, q *ObjectQueue)

func add(toAdd client.Object, wantLen int) action {
	return func(t *testing.T, q *ObjectQueue) {
		t.Helper()
		q.Add(toAdd)
		if q.Len() != wantLen {
			t.Errorf("got length %d after adding object %q; want length %d", q.Len(), core.IDOf(toAdd), wantLen)
		}
	}
}

func get(wantObj client.Object, wantLen int) action {
	return func(t *testing.T, q *ObjectQueue) {
		t.Helper()
		got, shutdown := q.Get()
		if shutdown {
			t.Fatal("unexpected shutdown of queue")
		}
		if diff := cmp.Diff(got, wantObj); diff != "" {
			t.Errorf("unexpected object from queue; diff: %s", diff)
		}
		if q.Len() != wantLen {
			t.Errorf("got length %d after getting from queue; want length %d", q.Len(), wantLen)
		}
	}
}

func done(toDone client.Object, wantLen int) action {
	return func(t *testing.T, q *ObjectQueue) {
		t.Helper()
		q.Done(toDone)
		if q.Len() != wantLen {
			t.Errorf("got length %d after marking object %q done; want length %d", q.Len(), core.IDOf(toDone), wantLen)
		}
	}
}

func TestObjectQueue(t *testing.T) {
	cmHelloGen0 := fake.ConfigMapObject(core.Namespace("foo-ns"), core.Name("hello"))
	cmHelloGen1 := fake.ConfigMapObject(core.Namespace("foo-ns"), core.Name("hello"), core.Generation(1))
	cmGoodbyeGen0 := fake.ConfigMapObject(core.Namespace("foo-ns"), core.Name("goodbye"))
	cmGoodbyeGen1 := fake.ConfigMapObject(core.Namespace("foo-ns"), core.Name("goodbye"), core.Generation(1))

	testCases := []struct {
		name    string
		actions []action
	}{
		{
			name: "add cmHello, get, done",
			actions: []action{
				add(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				done(cmHelloGen0, 0),
			},
		},
		{
			name: "add cmHello, update cmHello, get, done",
			actions: []action{
				add(cmHelloGen0, 1),
				add(cmHelloGen1, 1),
				get(cmHelloGen1, 0),
				done(cmHelloGen1, 0),
			},
		},
		{
			name: "add cmHello, ignore current cmHello, get, done",
			actions: []action{
				add(cmHelloGen0, 1),
				add(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				done(cmHelloGen0, 0),
			},
		},
		{
			name: "add cmHello, ignore old cmHello, get, done",
			actions: []action{
				add(cmHelloGen1, 1),
				add(cmHelloGen0, 1),
				get(cmHelloGen1, 0),
				done(cmHelloGen1, 0),
			},
		},
		{
			name: "add cmHello, add cmGoodBye, get, get, done, done",
			actions: []action{
				add(cmHelloGen0, 1),
				add(cmGoodbyeGen0, 2),
				get(cmHelloGen0, 1),
				get(cmGoodbyeGen0, 0),
				done(cmHelloGen0, 0),
				done(cmGoodbyeGen0, 0),
			},
		},
		{
			name: "add cmHello, get, add cmGoodBye, get, done, done",
			actions: []action{
				add(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				add(cmGoodbyeGen0, 1),
				get(cmGoodbyeGen0, 0),
				done(cmHelloGen0, 0),
				done(cmGoodbyeGen0, 0),
			},
		},
		{
			name: "add cmHello, get, update cmHello, done, get, done",
			actions: []action{
				add(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				add(cmHelloGen1, 0),
				done(cmHelloGen0, 1),
				get(cmHelloGen1, 0),
				done(cmHelloGen1, 0),
			},
		},
		{
			name: "add cmHello, get, requeue cmHello, done, get, done",
			actions: []action{
				add(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				add(cmHelloGen0, 0),
				done(cmHelloGen0, 1),
				get(cmHelloGen0, 0),
				done(cmHelloGen0, 0),
			},
		},
		{
			name: "add cmHello, get, ignore old cmHello, done",
			actions: []action{
				add(cmHelloGen1, 1),
				get(cmHelloGen1, 0),
				add(cmHelloGen0, 0),
				done(cmHelloGen1, 0),
			},
		},
		{
			name: "stress the logic",
			actions: []action{
				add(cmHelloGen0, 1),    // add the initial hello   [hello0]
				add(cmGoodbyeGen1, 2),  // add the initial goodbye   [hello0, goodbye1]
				add(cmGoodbyeGen0, 2),  // ignore the out-of-date goodbye   [hello0, goodbye1]
				get(cmHelloGen0, 1),    // read the initial hello   [goodbye1]
				add(cmHelloGen0, 1),    // re-add the same hello   [goodbye1] (hello0)
				get(cmGoodbyeGen1, 0),  // read the initial goodbye   [] (hello0)
				add(cmGoodbyeGen1, 0),  // re-add the same goodbye   [] (hello0, goodbye1)
				done(cmGoodbyeGen1, 1), // complete the initial goodbye and requeue   [goodbye1] (hello0)
				add(cmHelloGen1, 1),    // update dirty hello   [goodbye1] (hello1)
				done(cmHelloGen0, 2),   // complete the initial hello and requeue   [goodbye1, hello1]
				get(cmGoodbyeGen1, 1),  // read the requeued goodbye   [hello1]
				done(cmGoodbyeGen1, 1), // complete the requeued goodbye   [hello1]
				get(cmHelloGen1, 0),    // read the updated hello   []
				done(cmHelloGen1, 0),   // complete the updated hello   []
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q := New("test")
			for _, actAndVerify := range tc.actions {
				actAndVerify(t, q)
			}
			q.ShutDown()
		})
	}
}
