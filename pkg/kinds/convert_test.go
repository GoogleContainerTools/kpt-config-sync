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

package kinds

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestToTypedObject(t *testing.T) {
	emptyScheme := runtime.NewScheme()
	coreScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(coreScheme); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name          string
		object        runtime.Object
		scheme        *runtime.Scheme
		expected      runtime.Object
		expectedError error
	}{
		{
			name: "unstructured pre-populated GVK not in scheme",
			object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
			scheme: emptyScheme,
			expectedError: testutil.EqualError(
				fmt.Errorf("type not registered with scheme: %v", Service())),
		},
		{
			name: "unstructured pre-populated GVK in scheme",
			object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
		},
		{
			name: "typed pre-populated GVK not in scheme",
			object: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: emptyScheme,
			expectedError: testutil.EqualError(
				errors.Wrap(
					runtime.NewNotRegisteredErrForType(emptyScheme.Name(),
						reflect.TypeOf(corev1.Service{})),
					"failed to lookup object type")),
		},
		{
			name: "typed pre-populated GVK in scheme",
			object: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
		},
		{
			name: "typed unpopulated GVK not in scheme",
			object: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: emptyScheme,
			expectedError: testutil.EqualError(
				errors.Wrap(
					runtime.NewNotRegisteredErrForType(emptyScheme.Name(),
						reflect.TypeOf(corev1.Service{})),
					"failed to lookup object type")),
		},
		{
			name: "typed unpopulated GVK in scheme",
			object: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ToTypedObject(tc.object, tc.scheme)
			testutil.AssertEqual(t, tc.expectedError, err)
			testutil.AssertEqual(t, tc.expected, actual)
		})
	}
}

func TestToUnstructured(t *testing.T) {
	emptyScheme := runtime.NewScheme()
	coreScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(coreScheme); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name          string
		object        runtime.Object
		scheme        *runtime.Scheme
		expected      *unstructured.Unstructured
		expectedError error
	}{
		{
			name: "unstructured pre-populated GVK not in scheme",
			object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
			scheme: emptyScheme,
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
		},
		{
			name: "unstructured pre-populated GVK in scheme",
			object: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),
								"targetPort": int64(9376),
							},
						},
					},
				},
			},
		},
		{
			name: "typed pre-populated GVK not in scheme",
			object: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: emptyScheme,
			expectedError: testutil.EqualError(
				errors.Wrap(
					runtime.NewNotRegisteredErrForType(emptyScheme.Name(),
						reflect.TypeOf(corev1.Service{})),
					"failed to lookup object type")),
		},
		{
			name: "typed pre-populated GVK in scheme",
			object: &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: Service().GroupVersion().String(),
					Kind:       Service().Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name":              "test-name",
						"creationTimestamp": nil, // Added field
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),   // Type change
								"targetPort": int64(9376), // Type change
							},
						},
					},
					"status": map[string]interface{}{ // Added field
						"loadBalancer": map[string]interface{}{}, // Added field
					},
				},
			},
		},
		{
			name: "typed unpopulated GVK not in scheme",
			object: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: emptyScheme,
			expectedError: testutil.EqualError(
				errors.Wrap(
					runtime.NewNotRegisteredErrForType(emptyScheme.Name(),
						reflect.TypeOf(corev1.Service{})),
					"failed to lookup object type")),
		},
		{
			name: "typed unpopulated GVK in scheme",
			object: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/name": "MyApp",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(80),
							TargetPort: intstr.FromInt(9376),
						},
					},
				},
			},
			scheme: coreScheme,
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": Service().GroupVersion().String(),
					"kind":       Service().Kind,
					"metadata": map[string]interface{}{
						"name": "test-name",
						// Nil struct pointers are always populated
						// due to an impl detail of Golang json.Marshal.
						// https://github.com/golang/go/issues/22480
						"creationTimestamp": nil, // Added field
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"app.kubernetes.io/name": "MyApp",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"protocol":   "TCP",
								"port":       int64(80),   // Type change
								"targetPort": int64(9376), // Type change
							},
						},
					},
					// Empty struct maps are always populated
					// due to a impl detail of Golang json.Marshal.
					// That behavior was copied into the reflection-based method
					// that runtime.UnstructuredConverter uses for consistency.
					// These struct fields do have omitempty/optional specified,
					// but it's ignored by the json & convert libraries.
					// https://github.com/golang/go/issues/10648
					// https://github.com/golang/go/issues/11939
					// https://github.com/golang/go/issues/45669
					// https://github.com/golang/go/issues/22480
					"status": map[string]interface{}{ // Added field
						"loadBalancer": map[string]interface{}{}, // Added field
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ToUnstructured(tc.object, tc.scheme)
			testutil.AssertEqual(t, tc.expectedError, err)
			testutil.AssertEqual(t, tc.expected, actual)
		})
	}
}
