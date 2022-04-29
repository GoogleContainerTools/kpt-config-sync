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

package e2e

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSyncingThroughAProxy(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo)

	nt.T.Logf("Set up the tiny proxy service and Override the RootSync object with proxy setting")
	nt.MustKubectl("apply", "-f", "../testdata/proxy")
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", "../testdata/proxy")
		_, err := nomostest.Retry(nt.DefaultWaitTimeout, func() error {
			return nt.ValidateNotFound("tinyproxy-deployment", "proxy-test", &appsv1.Deployment{})
		})
		if err != nil {
			nt.T.Fatal(err)
		}
	})
	_, err := nomostest.Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate("tinyproxy-deployment", "proxy-test", &appsv1.Deployment{}, hasReadyReplicas(1))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the NoOpProxyError")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Validation", `KNV1061: RootSyncs which declare spec.git.proxy must declare spec.git.auth="none", "cookiefile" or "token"`)

	nt.T.Log("Set auth type to cookiefile")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "cookiefile"}}}`)
	nt.T.Log("Verify the secretRef error")
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Secret", `git secretType was set as "cookiefile" but cookie_file key is not present`)

	nt.T.Log("Set auth type to token")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "token"}}}`)
	nt.T.Log("Verify the secretRef error")
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Secret", `git secretType was set as "token" but token key is not present`)

	nt.T.Log("Set auth type to none")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "none", "secretRef": {"name":""}}}}`)
	nt.T.Log("Verify no errors")
	rs = &v1beta1.RootSync{}
	if err = nt.Get("root-sync", configmanagement.ControllerNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRepoRootSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "foo-corp"}))
}

func hasReadyReplicas(replicas int32) nomostest.Predicate {
	return func(o client.Object) error {
		deployment, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(deployment, &appsv1.Deployment{})
		}
		actual := deployment.Status.ReadyReplicas
		if replicas != actual {
			return fmt.Errorf("expected %d ready replicas, but got %d", replicas, actual)
		}
		return nil
	}
}
