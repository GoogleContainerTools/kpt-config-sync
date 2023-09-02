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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/decode"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCRDs returns the names and CustomResourceDefinitions of the CRDs in ClusterConfig.
func GetCRDs(decoder decode.Decoder, clusterConfig *v1.ClusterConfig) ([]*apiextensionsv1.
	CustomResourceDefinition, status.Error) {
	if clusterConfig == nil {
		return nil, nil
	}

	gvkrs, err := decoder.DecodeResources(clusterConfig.Spec.Resources)
	if err != nil {
		return nil, status.APIServerErrorf(err, "could not deserialize CRD in %s", v1.CRDClusterConfigName)
	}

	crdMap := make(map[string]*apiextensionsv1.CustomResourceDefinition)
	for gvk, unstructureds := range gvkrs {
		if gvk != kinds.CustomResourceDefinition() {
			return nil, status.APIServerErrorf(err, "%s contains non-CRD resources: %v", v1.CRDClusterConfigName, gvk)
		}
		for _, u := range unstructureds {
			crd, err := AsCRD(u)
			if err != nil {
				return nil, err
			}
			crdMap[crd.GetName()] = crd
		}
	}

	var crds []*apiextensionsv1.CustomResourceDefinition
	for _, crd := range crdMap {
		crds = append(crds, crd)
	}
	return crds, nil
}

// MalformedCRDErrorCode is the error code for MalformedCRDError.
const MalformedCRDErrorCode = "1065"

var malformedCRDErrorBuilder = status.NewErrorBuilder(MalformedCRDErrorCode)

// MalformedCRDError reports a malformed CRD.
func MalformedCRDError(err error, obj client.Object) status.Error {
	return malformedCRDErrorBuilder.Wrap(err).
		Sprint("malformed CustomResourceDefinition").
		BuildWithResources(obj)
}

// AsCRD returns the typed version of the CustomResourceDefinition passed in.
func AsCRD(o *unstructured.Unstructured) (*apiextensionsv1.CustomResourceDefinition, status.Error) {
	obj, err := kinds.ToTypedWithVersion(o, kinds.CustomResourceDefinition(), core.Scheme)
	if err != nil {
		return nil, MalformedCRDError(err, o)
	}
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return nil, MalformedCRDError(fmt.Errorf("unexpected type produced by converting unstructured CRD to v1beta1 CRD: %T", obj), o)
	}
	return crd, nil
}
