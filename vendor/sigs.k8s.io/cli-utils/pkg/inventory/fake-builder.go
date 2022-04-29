// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/cli-utils/pkg/object"
)

var (
	codec       = scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	cmPathRegex = regexp.MustCompile(`^/namespaces/([^/]+)/configmaps$`)
)

// FakeBuilder encapsulates a resource Builder which will hard-code the return
// of an inventory object with the encoded past invObjs.
type FakeBuilder struct {
	invObjs object.ObjMetadataSet
}

// SetInventoryObjs sets the objects which will be encoded in
// an inventory object to be returned when queried for the cluster
// inventory object.
func (fb *FakeBuilder) SetInventoryObjs(objs object.ObjMetadataSet) {
	fb.invObjs = objs
}

// Returns the fake resource Builder with the fake client, test restmapper,
// and the fake category expander.
func (fb *FakeBuilder) GetBuilder() func() *resource.Builder {
	return func() *resource.Builder {
		return resource.NewFakeBuilder(
			fakeClient(fb.invObjs),
			func() (meta.RESTMapper, error) {
				return testrestmapper.TestOnlyStaticRESTMapper(scheme.Scheme), nil
			},
			func() (restmapper.CategoryExpander, error) {
				return resource.FakeCategoryExpander, nil
			})
	}
}

// fakeClient hard codes the return of an inventory object that encodes the passed
// objects into the inventory object when a GET of configmaps is called.
func fakeClient(objs object.ObjMetadataSet) resource.FakeClientFunc {
	return func(version schema.GroupVersion) (resource.RESTClient, error) {
		return &fake.RESTClient{
			NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
			Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
				if req.Method == "POST" && cmPathRegex.Match([]byte(req.URL.Path)) {
					b, err := ioutil.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					cm := corev1.ConfigMap{}
					err = runtime.DecodeInto(codec, b, &cm)
					if err != nil {
						return nil, err
					}
					bodyRC := ioutil.NopCloser(bytes.NewReader(b))
					return &http.Response{StatusCode: http.StatusCreated, Header: cmdtesting.DefaultHeader(), Body: bodyRC}, nil
				}
				if req.Method == "GET" && cmPathRegex.Match([]byte(req.URL.Path)) {
					cmList := corev1.ConfigMapList{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "List",
						},
						Items: []corev1.ConfigMap{},
					}
					var cm = corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "inventory",
							Namespace: "test-namespace",
						},
						Data: objs.ToStringMap(),
					}
					cmList.Items = append(cmList.Items, cm)
					bodyRC := ioutil.NopCloser(bytes.NewReader(toJSONBytes(&cmList)))
					return &http.Response{StatusCode: http.StatusOK, Header: cmdtesting.DefaultHeader(), Body: bodyRC}, nil
				}
				return nil, nil
			}),
		}, nil
	}
}

func toJSONBytes(obj runtime.Object) []byte {
	objBytes, _ := runtime.Encode(unstructured.NewJSONFallbackEncoder(codec), obj)
	return objBytes
}
