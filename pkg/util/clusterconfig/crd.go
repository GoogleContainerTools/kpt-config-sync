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

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/decode"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetCRDs returns the names and CustomResourceDefinitions of the CRDs in ClusterConfig.
func GetCRDs(decoder decode.Decoder, clusterConfig *v1.ClusterConfig) ([]*apiextensionsv1beta1.
	CustomResourceDefinition, status.Error) {
	if clusterConfig == nil {
		return nil, nil
	}

	gvkrs, err := decoder.DecodeResources(clusterConfig.Spec.Resources)
	if err != nil {
		return nil, status.APIServerErrorf(err, "could not deserialize CRD in %s", v1.CRDClusterConfigName)
	}

	crdMap := make(map[string]*apiextensionsv1beta1.CustomResourceDefinition)
	for gvk, unstructureds := range gvkrs {
		if gvk.GroupKind() != kinds.CustomResourceDefinition() {
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

	var crds []*apiextensionsv1beta1.CustomResourceDefinition
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
func AsCRD(o *unstructured.Unstructured) (*apiextensionsv1beta1.CustomResourceDefinition, status.Error) {
	if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1Beta1() {
		s, err := core.RemarshalToStructured(o)
		if err != nil {
			return nil, MalformedCRDError(err, o)
		}
		return s.(*apiextensionsv1beta1.CustomResourceDefinition), nil
	}
	if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1() {
		s, err := core.RemarshalToStructured(o)
		if err != nil {
			return nil, MalformedCRDError(err, o)
		}
		return asV1Beta1CRD(s.(*apiextensionsv1.CustomResourceDefinition))
	}

	return nil, MalformedCRDError(fmt.Errorf("could not generate a CRD from %T: %#v", o, o), o)
}

// asV1Beta1CRD converts a v1 CRD to a v1beta1 CRD.
func asV1Beta1CRD(crdV1 *apiextensionsv1.CustomResourceDefinition) (*apiextensionsv1beta1.CustomResourceDefinition, status.Error) {
	// Use the apiextensions conversion functions to convert to a v1beta1 CRD.
	crd := &apiextensions.CustomResourceDefinition{}
	err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(crdV1, crd, nil)
	if err != nil {
		return nil, MalformedCRDError(errors.Wrapf(err, "could not generate an extension CRD from %T: %#v", crdV1, crdV1), crdV1)
	}
	crdV1Beta1 := &apiextensionsv1beta1.CustomResourceDefinition{}
	err = apiextensionsv1beta1.Convert_apiextensions_CustomResourceDefinition_To_v1beta1_CustomResourceDefinition(crd, crdV1Beta1, nil)
	if err != nil {
		return nil, MalformedCRDError(errors.Wrapf(err, "could not generate a v1beta1 CRD from %T: %#v", crd, crd), crdV1)
	}
	return crdV1Beta1, nil
}

// AsV1CRD returns the typed version of the CustomResourceDefinition passed in.
func AsV1CRD(o *unstructured.Unstructured) (*apiextensionsv1.CustomResourceDefinition, status.Error) {
	if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1Beta1() {
		s, err := core.RemarshalToStructured(o)
		if err != nil {
			return nil, MalformedCRDError(err, o)
		}
		return V1Beta1ToV1CRD(s.(*apiextensionsv1beta1.CustomResourceDefinition))
	}
	if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1() {
		s, err := core.RemarshalToStructured(o)
		if err != nil {
			return nil, MalformedCRDError(err, o)
		}
		return s.(*apiextensionsv1.CustomResourceDefinition), nil
	}

	return nil, MalformedCRDError(fmt.Errorf("could not generate a CRD from %T: %#v", o, o), o)
}

// V1Beta1ToV1CRD converts a v1beta1 CRD to a v1 CRD.
func V1Beta1ToV1CRD(crdV1Beta1 *apiextensionsv1beta1.CustomResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, status.Error) {
	// Use the apiextensions conversion functions to convert to a v1 CRD.
	crd := &apiextensions.CustomResourceDefinition{}
	err := apiextensionsv1beta1.Convert_v1beta1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(crdV1Beta1, crd, nil)
	if err != nil {
		return nil, MalformedCRDError(errors.Wrapf(err, "could not generate an extension CRD from %T: %#v", crdV1Beta1, crdV1Beta1), crdV1Beta1)
	}
	crdV1 := &apiextensionsv1.CustomResourceDefinition{}
	err = apiextensionsv1.Convert_apiextensions_CustomResourceDefinition_To_v1_CustomResourceDefinition(crd, crdV1, nil)
	if err != nil {
		return nil, MalformedCRDError(errors.Wrapf(err, "could not generate a v1 CRD from %T: %#v", crd, crd), crdV1Beta1)
	}
	return crdV1, nil
}
