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

package core

import (
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	k8sadmissionv1 "k8s.io/kubernetes/pkg/apis/admission/v1"
	k8sappsv1 "k8s.io/kubernetes/pkg/apis/apps/v1"
	k8scorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	k8snetworkingv1 "k8s.io/kubernetes/pkg/apis/networking/v1"
	k8spolicyv1beta1 "k8s.io/kubernetes/pkg/apis/policy/v1beta1"
	"k8s.io/kubernetes/pkg/apis/rbac"
	k8srbacv1 "k8s.io/kubernetes/pkg/apis/rbac/v1"
	k8srbacv1beta1 "k8s.io/kubernetes/pkg/apis/rbac/v1beta1"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	configsyncv1alpha1 "kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	kptv1alpha1 "kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
)

// Scheme is a reference to the global scheme.
// Use this Scheme to ensure you have all the required types added.
var Scheme = scheme.Scheme

// init registers all the k8s resource types needed for runtime and unit tests.
//
// Only the resources needed at compile time are required to be registered.
// Config Sync does not use the scheme for parsing objects from Git/OCI/Helm.
//
// When there are multiple versions registered for an API group, be sure to
// SetVersionPriority to specify which to use by default. This is used by the
// fake clients to determine which version to store.
//
// If possible, also register conversion code. This allows the fake clients to
// do real version conversion, instead of only copying matching fields.
func init() {
	mustRegisterKubernetesResources()
	mustRegisterAPIExtensionsResources()

	// Config Sync types
	utilruntime.Must(clusterregistry.AddToScheme(scheme.Scheme))

	utilruntime.Must(configmanagementv1.AddToScheme(scheme.Scheme))

	utilruntime.Must(configsyncv1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(configsyncv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(configsyncv1alpha1.RegisterConversions(scheme.Scheme))
	utilruntime.Must(scheme.Scheme.SetVersionPriority(configsyncv1beta1.SchemeGroupVersion, configsyncv1alpha1.SchemeGroupVersion))

	// Kpt types
	utilruntime.Must(kptv1alpha1.AddToScheme(scheme.Scheme))

	// Hub/Fleet types
	utilruntime.Must(hubv1.AddToScheme(scheme.Scheme))
}

func mustRegisterKubernetesResources() {
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8scorev1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8scorev1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(appsv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8sappsv1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8sappsv1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(admissionv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8sadmissionv1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8sadmissionv1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(policyv1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8spolicyv1beta1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8spolicyv1beta1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(networkingv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8snetworkingv1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8snetworkingv1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(rbac.AddToScheme(scheme.Scheme))

	utilruntime.Must(rbacv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8srbacv1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8srbacv1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(rbacv1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(k8srbacv1beta1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(k8srbacv1beta1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(scheme.Scheme.SetVersionPriority(rbacv1.SchemeGroupVersion, rbacv1beta1.SchemeGroupVersion))
}

func mustRegisterAPIExtensionsResources() {
	utilruntime.Must(apiextensions.AddToScheme(Scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(apiextensionsv1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(apiextensionsv1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(apiextensionsv1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(apiextensionsv1beta1.RegisterDefaults(scheme.Scheme))
	utilruntime.Must(apiextensionsv1beta1.RegisterConversions(scheme.Scheme))

	utilruntime.Must(scheme.Scheme.SetVersionPriority(apiextensionsv1.SchemeGroupVersion, apiextensionsv1beta1.SchemeGroupVersion))
}
