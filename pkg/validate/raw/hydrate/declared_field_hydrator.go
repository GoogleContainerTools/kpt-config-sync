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

package hydrate

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
)

// DeclaredFields hydrates the given Raw objects by annotating each object with
// its fields that are declared in Git. This annotation is what enables the
// Config Sync admission controller webhook to protect these declared fields
// from being changed by another controller or user.
func DeclaredFields(objs *objects.Raw) status.MultiError {
	if objs.Converter == nil {
		klog.Warning("Skipping declared field hydration. This should only happen for offline executions of nomos vet/hydrate/init.")
		return nil
	}

	var errs status.MultiError
	needRefresh := false
	for _, obj := range objs.Objects {
		fields, err := encodeDeclaredFields(objs.Converter, obj.Unstructured)
		if err != nil {
			errs = status.Append(errs, status.EncodeDeclaredFieldError(obj.Unstructured, err))
			// This error could be due to an out of date schema.
			// So the converter needs to be refreshed.
			needRefresh = true
		}
		core.SetAnnotation(obj, metadata.DeclaredFieldsKey, string(fields))
	}

	if needRefresh {
		// Refresh the converter so that the new schema of types can be used in the next loop of parsing/validating.
		// If the error returned by `encodeDeclaredFields` is due to the
		// out of date schema in the Converter, it will be gone in the next loop of hydration/validation.
		klog.Info("Got error from encoding declared fields. It might be due to an out of date schemas. Refreshing the schemas from the discovery client")
		if err := objs.Converter.Refresh(); err != nil {
			// No special handling for the error here.
			// If Refresh function fails, the next loop of hydration/validation will trigger it again.
			klog.Warningf("failed to refresh the schemas %v", err)
		}
	}
	return errs
}

// identityFields are the fields in an object which identify it and therefore
// would never mutate.
var identityFields = fieldpath.NewSet(
	fieldpath.MakePathOrDie("apiVersion"),
	fieldpath.MakePathOrDie("kind"),
	fieldpath.MakePathOrDie("metadata"),
	fieldpath.MakePathOrDie("metadata", "name"),
	fieldpath.MakePathOrDie("metadata", "namespace"),
	// TODO: Remove the following fields. They should never be
	//  allowed in Git, but currently our unit test fakes can generate them so we
	//  need to sanitize them until we have more Unstructured fakes for unit tests.
	fieldpath.MakePathOrDie("metadata", "creationTimestamp"),
)

// encodeDeclaredFields encodes the fields of the given object into a format that
// is compatible with server-side apply.
func encodeDeclaredFields(converter *declared.ValueConverter, obj runtime.Object) ([]byte, error) {
	var err error
	val, err := converter.TypedValue(obj)
	if err != nil {
		return nil, err
	}
	set, err := val.ToFieldSet()
	if err != nil {
		return nil, err
	}
	// Strip identity fields away since changing them would change the identity of
	// the object.
	set = set.Difference(identityFields)
	return set.ToJSON()
}
