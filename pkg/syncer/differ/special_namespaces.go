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

package differ

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/policycontroller"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SpecialNamespaces tracks the namespaces ACM should never remove from a cluster.
var SpecialNamespaces = map[string]bool{
	// default is the "" namespace.
	metav1.NamespaceDefault: true,
	// kube-system runs kubernetes system pods.
	metav1.NamespaceSystem: true,
	// kube-public is a namespace created by kubeadm.
	metav1.NamespacePublic: true,
	// kube-node-lease contains one Lease object per node, which is the new way to implements node heartbeat.
	corev1.NamespaceNodeLease: true,
	// gatekeeper-system should never be deleted by ACM no matter how it was installed.
	policycontroller.NamespaceSystem: true,
}

// IsManageableSystemNamespace returns if the input namespace is a manageable system namespace.
func IsManageableSystemNamespace(o client.Object) bool {
	if o.GetObjectKind().GroupVersionKind().GroupKind() != kinds.Namespace().GroupKind() {
		return false
	}
	return SpecialNamespaces[o.GetName()]
}
