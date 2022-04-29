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

package webhook

import (
	"fmt"
	"strings"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
)

// ObjectDiffer can compare two versions of an Object and report on any declared
// fields which were modified between the two.
type ObjectDiffer struct {
	converter *declared.ValueConverter
}

// FieldSet returns a Set of the fields in the given Object.
func (d *ObjectDiffer) FieldSet(obj client.Object) (*fieldpath.Set, error) {
	value, err := d.converter.TypedValue(obj)
	if err != nil {
		return nil, err
	}
	return value.ToFieldSet()
}

// FieldDiff returns a Set of the Object fields which are being modified
// in the given Request that are also marked as fields declared in Git.
func (d *ObjectDiffer) FieldDiff(oldObj, newObj client.Object) (*fieldpath.Set, error) {
	oldValue, err := d.converter.TypedValue(oldObj)
	if err != nil {
		return nil, err
	}
	newValue, err := d.converter.TypedValue(newObj)
	if err != nil {
		return nil, err
	}
	cmp, err := oldValue.Compare(newValue)
	if err != nil {
		return nil, err
	}

	// We only check fields that were modified or removed. We don't care about
	// fields that are added since they will never overlap with declared fields.
	return cmp.Modified.Union(cmp.Removed).Union(cmp.Added), nil
}

var (
	metadata     = "metadata"
	annotations  = ".annotations."
	labels       = ".labels."
	metadataPath = fieldpath.PathElement{FieldName: &metadata}
)

// ConfigSyncMetadata returns all of the metadata fields in the given fieldpath
// Set which are ConfigSync labels or annotations.
func ConfigSyncMetadata(set *fieldpath.Set) *fieldpath.Set {
	metadataSet := set.WithPrefix(metadataPath)

	csSet := fieldpath.NewSet()
	metadataSet.Iterate(func(path fieldpath.Path) {
		s := path.String()
		if strings.HasPrefix(s, annotations) {
			s = s[len(annotations):]
			if csmetadata.IsConfigSyncAnnotationKey(s) {
				csSet.Insert(path)
			}
		} else if strings.HasPrefix(s, labels) {
			s = s[len(labels):]
			if csmetadata.IsConfigSyncLabelKey(s) {
				csSet.Insert(path)
			}
		}
	})
	return csSet
}

// DeclaredFields returns the declared fields for the given Object.
func DeclaredFields(obj client.Object) (*fieldpath.Set, error) {
	decls, ok := obj.GetAnnotations()[csmetadata.DeclaredFieldsKey]
	if !ok {
		return nil, fmt.Errorf("%s annotation is missing from %s", csmetadata.DeclaredFieldsKey, core.GKNN(obj))
	}

	set := &fieldpath.Set{}
	if err := set.FromJSON(strings.NewReader(decls)); err != nil {
		return nil, err
	}
	return set, nil
}
