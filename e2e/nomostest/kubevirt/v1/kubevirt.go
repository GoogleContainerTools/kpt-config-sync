// Copyright 2023 Google LLC
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

// Package kubevirt is a simple library of constants to avoid needing to import
// the whole KubeVirt SDK.
package kubevirt

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Group used by KubeVirt resources
const Group = "kubevirt.io"

// Version used by KubeVirt v1 resources
const Version = "v1"

// KindKubeVirt is the kind used by KubeVirt resources
const KindKubeVirt = "KubeVirt"

// KindVirtualMachine is the kind used by VirtualMachine resources
const KindVirtualMachine = "VirtualMachine"

// ResourceKubeVirts is the resource name used to query KubeVirt resources
const ResourceKubeVirts = "kubevirts"

// ResourceVirtualMachines is the resource name used to query VirtualMachine resources
const ResourceVirtualMachines = "virtualmachines"

// GVKKubeVirt returns the GroupVersionKind for the KubeVirt resource
func GVKKubeVirt() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   Group,
		Version: Version,
		Kind:    KindKubeVirt,
	}
}

// GVKVirtualMachine returns a the GroupVersionKind for the VirtualMachine resource
func GVKVirtualMachine() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   Group,
		Version: Version,
		Kind:    KindVirtualMachine,
	}
}

// NewKubeVirtObject returns a new Unstructured object for the KubeVirt resource
func NewKubeVirtObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(GVKKubeVirt())
	return obj
}

// NewVirtualMachineObject returns a new Unstructured object for the VirtualMachine resource
func NewVirtualMachineObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(GVKVirtualMachine())
	return obj
}
