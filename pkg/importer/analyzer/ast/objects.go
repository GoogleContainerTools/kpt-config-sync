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

package ast

import (
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewFileObject returns an ast.FileObject with the specified underlying
// client.Object and the designated source file.
// TODO: This function should accept an unstructured.Unstructured
// if possible. Also we should see if we can make FileObject *not* implement
// client.Object and instead make callers explicitly interact with one format or
// the other.
func NewFileObject(object *unstructured.Unstructured, source cmpath.Relative) FileObject {
	return FileObject{
		Unstructured: object,
		Relative:     source,
	}
}

// FileObject extends client.Object to include the path to the file in the repo.
type FileObject struct {
	// The unstructured representation of the object.
	*unstructured.Unstructured
	// Relative is the path of this object in the repo prefixed by the Nomos Root.
	cmpath.Relative
}

var _ client.Object = &FileObject{}

// CompareFileObject is a cmp.Option which allows tests to compare FileObjects.
var CompareFileObject = cmp.AllowUnexported(FileObject{})

// DeepCopy returns a deep copy of the FileObject.
func (o *FileObject) DeepCopy() FileObject {
	return FileObject{
		Unstructured: o.Unstructured.DeepCopy(),
		Relative:     o.Relative,
	}
}

// Structured returns the structured representation of the object. This can be
// cast to a golang struct (eg v1.CustomResourceDefinition) for validation and
// hydration logic. Note that the structured object should only be read. No
// mutations to the structured object (eg in hydration) will be persisted.
// Unmarshalling and re-marshalling an object can result in spurious JSON fields
// depending on what directives are specified for those  fields. To be safe, we
// keep all resources in their raw unstructured format.  If hydration or
// validation code requires the structured format, we can convert it here
// separate from the raw unstructured representation.
func (o *FileObject) Structured() (runtime.Object, status.Error) {
	obj, err := core.RemarshalToStructured(o.Unstructured)
	if err != nil {
		return nil, status.ObjectParseError(o.Unstructured, err)
	}
	return obj, nil
}
