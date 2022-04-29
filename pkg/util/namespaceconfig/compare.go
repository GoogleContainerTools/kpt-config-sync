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

package namespaceconfig

import (
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/util/compare"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func resourceQuantityCmp(lhs, rhs resource.Quantity) bool {
	return lhs.Cmp(rhs) == 0
}

var ncsIgnore = []cmp.Option{
	// Quantity has a few unexported fields which we need to manually compare. The path is:
	// ResourceQuota -> ResourceQuotaSpec -> ResourceList -> Quantity
	cmp.Comparer(resourceQuantityCmp),
}

// NamespaceConfigsEqual returns true if the NamespaceConfigs are equivalent.
func NamespaceConfigsEqual(decoder decode.Decoder, lhs client.Object, rhs client.Object) (bool, error) {
	l := lhs.(*v1.NamespaceConfig)
	r := rhs.(*v1.NamespaceConfig)

	resourceEqual, err := compare.GenericResourcesEqual(decoder, l.Spec.Resources, r.Spec.Resources, ncsIgnore...)
	if err != nil {
		return false, err
	}
	metaEqual := compare.ObjectMetaEqual(l, r)
	// We only care about the DeleteSyncedTime field in .spec; all the other fields are
	// expected to change in between reconciles.
	namespaceConfigsEqual := resourceEqual && metaEqual && l.Spec.DeleteSyncedTime == r.Spec.DeleteSyncedTime
	return namespaceConfigsEqual, nil
}
