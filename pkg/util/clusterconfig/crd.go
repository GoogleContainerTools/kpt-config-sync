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

package clusterconfig

import (
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MalformedCRDErrorCode is the error code for MalformedCRDError.
const MalformedCRDErrorCode = "1065"

var malformedCRDErrorBuilder = status.NewErrorBuilder(MalformedCRDErrorCode)

// MalformedCRDError reports a malformed CRD.
func MalformedCRDError(err error, obj client.Object) status.Error {
	return malformedCRDErrorBuilder.Wrap(err).
		Sprint("malformed CustomResourceDefinition").
		BuildWithResources(obj)
}

// ToCRD converts an Unstructured object into a v1.CustomResourceDefinition
// using conversions registered with the specified scheme.
func ToCRD(o *unstructured.Unstructured, scheme *runtime.Scheme) (*apiextensionsv1.CustomResourceDefinition, status.Error) {
	obj, err := kinds.ToTypedWithVersion(o, kinds.CustomResourceDefinitionV1(), scheme)
	if err != nil {
		return nil, MalformedCRDError(err, o)
	}
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return nil, MalformedCRDError(fmt.Errorf("unexpected type produced by converting unstructured CRD to v1 CRD: %T", obj), o)
	}
	return crd, nil
}
