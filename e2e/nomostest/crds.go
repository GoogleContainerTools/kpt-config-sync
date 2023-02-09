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

package nomostest

import (
	"fmt"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	multiRepoCRDs = []string{
		"reposyncs.configsync.gke.io",
		"rootsyncs.configsync.gke.io",
		"resourcegroups.kpt.dev",
		// Shared CRDS
		"clusters.clusterregistry.k8s.io",
		"clusterselectors.configmanagement.gke.io",
		"namespaceselectors.configmanagement.gke.io",
	}
)

// WaitForCRDs waits until the specified CRDs are established on the cluster.
func WaitForCRDs(nt *NT, crds []string) error {
	tg := taskgroup.New()
	for _, crd := range crds {
		tg.Go(func() error {
			return WatchObject(nt, kinds.CustomResourceDefinitionV1(), crd, "",
				[]Predicate{IsEstablished})
		})
	}
	return tg.Wait()
}

// IsEstablished returns true if the given CRD is established on the cluster,
// which indicates if discovery knows about it yet. For more info see
// https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#create-a-customresourcedefinition
func IsEstablished(o client.Object) error {
	if o == nil {
		return ErrObjectNotFound
	}
	crd, ok := o.(*v1.CustomResourceDefinition)
	if !ok {
		return WrongTypeErr(o, crd)
	}

	for _, condition := range crd.Status.Conditions {
		if condition.Type == v1.Established {
			if condition.Status == v1.ConditionTrue {
				return nil
			}
		}
	}
	return fmt.Errorf("CRD %q is not established", crd.Name)
}
