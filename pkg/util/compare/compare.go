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

package compare

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/decode"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectMetaEqual returns true if the Meta field of left and right objects are equal.
func ObjectMetaEqual(left client.Object, right client.Object) bool {
	return reflect.DeepEqual(left.GetLabels(), right.GetLabels()) && reflect.DeepEqual(left.GetAnnotations(), right.GetAnnotations())
}

// GenericResourcesEqual returns true if the GenericResources slices are
// equivalent.
// Since the GenericResources in the cluster have the RawExtension.Raw field
// populated and the ones being generated have the RawExtension.Object field
// populated, we need to decode them to have a common representation for
// comparing the underlying resources.
func GenericResourcesEqual(decoder decode.Decoder, l []v1.GenericResources, r []v1.GenericResources,
	cmpOptions ...cmp.Option) (bool, error) {
	lr, lErr := decoder.DecodeResources(l)
	if lErr != nil {
		return false, status.InternalWrap(lErr)
	}

	rr, rErr := decoder.DecodeResources(r)
	if rErr != nil {
		return false, status.InternalWrap(rErr)
	}

	return cmp.Equal(lr, rr, cmpOptions...), nil
}
