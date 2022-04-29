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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetaMutator is a Mutator that modifies the metadata of an Object.
type MetaMutator func(o client.Object)

// Namespace replaces the metadata.namespace of the Object under test.
func Namespace(namespace string) MetaMutator {
	return func(o client.Object) {
		o.SetNamespace(namespace)
	}
}

// Name replaces the metadata.name of the Object under test.
func Name(name string) MetaMutator {
	return func(o client.Object) {
		o.SetName(name)
	}
}

// UID replaces the metadata.uid of the Object under test.
func UID(uid types.UID) MetaMutator {
	return func(o client.Object) {
		o.SetUID(uid)
	}
}

// Label adds label=value to the metadata.labels of the Object under test.
func Label(label, value string) MetaMutator {
	return func(o client.Object) {
		SetLabel(o, label, value)
	}
}

// Labels sets the object's labels to a copy of the passed map.
// Setting to nil causes a call to SetLabels(nil), but the underlying implementation may set Labels
// to empty map.
func Labels(labels map[string]string) MetaMutator {
	return func(o client.Object) {
		o.SetLabels(copyMap(labels))
	}
}

// Annotation adds annotation=value to the metadata.annotations of the MetaObject under test.
func Annotation(annotation, value string) MetaMutator {
	return func(o client.Object) {
		SetAnnotation(o, annotation, value)
	}
}

// WithoutAnnotation removes annotation from metadata.annotations of the MetaObject under test.
func WithoutAnnotation(annotation string) MetaMutator {
	return func(o client.Object) {
		RemoveAnnotations(o, annotation)
	}
}

// Annotations sets the object's annotations to a copy of the passed map.
// Setting to nil causes a call to SetAnnotations(nil), but the underlying implementation may set
// Annotations to empty map.
func Annotations(annotations map[string]string) MetaMutator {
	return func(o client.Object) {
		o.SetAnnotations(copyMap(annotations))
	}
}

// OwnerReference sets the object's owner references to a passed slice of metav1.OwnerReference.
func OwnerReference(or []metav1.OwnerReference) MetaMutator {
	return func(o client.Object) {
		o.SetOwnerReferences(or)
	}
}

// Generation replaces the metadata.generation of the Object under test.
func Generation(gen int64) MetaMutator {
	return func(o client.Object) {
		o.SetGeneration(gen)
	}
}

// SelfLink sets the object's self link to the given value.
func SelfLink(selfLink string) MetaMutator {
	return func(o client.Object) {
		o.SetSelfLink(selfLink)
	}
}

// ResourceVersion sets the object's resource version to the given value.
func ResourceVersion(version string) MetaMutator {
	return func(o client.Object) {
		o.SetResourceVersion(version)
	}
}

// CreationTimeStamp sets the object's creation time stamp to the given value.
func CreationTimeStamp(ts metav1.Time) MetaMutator {
	return func(o client.Object) {
		o.SetCreationTimestamp(ts)
	}
}

// DeletionTimestamp sets the object's deletion timestamp to the given value.
func DeletionTimestamp(ts metav1.Time) MetaMutator {
	return func(o client.Object) {
		o.SetDeletionTimestamp(&ts)
	}
}

// DeletionGracePeriod sets the object's deletion grace period to the given
// value.
func DeletionGracePeriod(seconds int64) MetaMutator {
	return func(o client.Object) {
		o.SetDeletionGracePeriodSeconds(&seconds)
	}
}
