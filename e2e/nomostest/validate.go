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

package nomostest

import (
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Validate returns an error if the indicated object does not exist.
//
// Validates the object against each of the passed Predicates, returning error
// if any Predicate fails.
func (nt *NT) Validate(name, namespace string, o client.Object, predicates ...Predicate) error {
	err := nt.Get(name, namespace, o)
	if err != nil {
		return err
	}
	for _, p := range predicates {
		err = p(o)
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidateNotFound returns an error if the indicated object exists.
//
// `o` must either be:
// 1) a struct pointer to the type of the object to search for, or
// 2) an unstructured.Unstructured with the type information filled in.
func (nt *NT) ValidateNotFound(name, namespace string, o client.Object) error {
	err := nt.Get(name, namespace, o)
	if err == nil {
		return errors.Errorf("%T %v %s/%s found", o, o.GetObjectKind().GroupVersionKind(), namespace, name)
	}
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ValidateNotFoundOrNoMatch returns an error if the indicated object is
// neither NotFound nor NoMatchFound (GVK not found).
//
// Use this instead of ValidateNotFound when deleting a CRD or APIService at the
// same time as a custom resource, to avoid the race between possible errors.
func (nt *NT) ValidateNotFoundOrNoMatch(name, namespace string, o client.Object) error {
	err := nt.Get(name, namespace, o)
	if err == nil {
		return errors.Errorf("%T %v %s/%s found", o, o.GetObjectKind().GroupVersionKind(), namespace, name)
	}
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	return err
}

// ValidateSyncObject validates the specified object satisfies the specified
// constraints.
//
// gvk specifies the GroupVersionKind of the object to retrieve and validate.
//
// name and namespace identify the specific object to check.
//
// predicates are functions that return an error if the object is invalid.
func (nt *NT) ValidateSyncObject(gvk schema.GroupVersionKind, name, namespace string,
	predicates ...Predicate) error {
	rObj, err := nt.scheme.New(gvk)
	if err != nil {
		return fmt.Errorf("%w: got unrecognized GVK %v", ErrWrongType, gvk)
	}
	cObj, ok := rObj.(client.Object)
	if !ok {
		// This means the GVK corresponded to a type registered in the Scheme
		// which is not a valid Kubernetes object. We expect the only way this
		// can happen is if gvk is for a List type, like NamespaceList.
		return errors.Wrapf(ErrWrongType, "trying to wait for List type to sync: %T", cObj)
	}
	return nt.Validate(name, namespace, cObj, predicates...)
}
