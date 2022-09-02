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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// GenerateFileObjects returns a set of file objects with unique file names and
// invalid fields removed from objects.
func GenerateFileObjects(multiCluster bool, objects ...ast.FileObject) []ast.FileObject {
	fileObjects := generateUniqueFileNames(flags.OutputFormat, multiCluster, objects...)
	Clean(fileObjects)
	return fileObjects
}

// PrintFlatOutput prints the hydrated output to a single file.
func PrintFlatOutput(output, extension string, fileObjects []ast.FileObject) error {
	objects := make([]*unstructured.Unstructured, len(fileObjects))
	for i, o := range fileObjects {
		objects[i] = o.Unstructured
	}

	return PrintFile(output, extension, objects)
}

// PrintDirectoryOutput prints the hydrated output to multiple files in a directory.
func PrintDirectoryOutput(output, extension string, fileObjects []ast.FileObject) error {
	files := make(map[string][]*unstructured.Unstructured)
	for _, obj := range fileObjects {
		u, err := toUnstructured(obj.Unstructured)
		if err != nil {
			return errors.Wrapf(err, "failed to convert the object %s/%s/%s to unstructured format", obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName())
		}
		files[obj.SlashPath()] = append(files[obj.SlashPath()], u)
	}

	for file, objects := range files {
		err := PrintFile(filepath.Join(output, file), extension, objects)
		if err != nil {
			return errors.Wrap(err, "failed to print file")
		}
	}
	return nil
}

// PrintFile prints the passed objects to file.
func PrintFile(file, extension string, objects []*unstructured.Unstructured) (err error) {
	err = os.MkdirAll(filepath.Dir(file), os.ModePerm)
	if err != nil {
		return err
	}

	outFile, err := os.Create(file)
	if err != nil {
		return err
	}

	defer func() {
		err2 := outFile.Close()
		if err2 != nil && err == nil {
			// Assign the named parameter since there's no other way to ensure we get
			// the error from the deferred Close.
			err = err2
		}
	}()

	var content string
	switch extension {
	case "yaml":
		content, err = toYAML(objects)
	case "json":
		content, err = toJSON(objects)
	}
	if err != nil {
		return err
	}
	_, err = outFile.WriteString(content)
	return err
}

func toUnstructured(obj client.Object) (*unstructured.Unstructured, error) {
	// Must convert or else fields like status automatically get written.
	uObj, err := kinds.ToUnstructured(obj, core.Scheme)
	if err != nil {
		return nil, err
	}
	rmBadFields(uObj)
	return uObj, nil
}

func toYAML(objects []*unstructured.Unstructured) (string, error) {
	content := strings.Builder{}
	for _, o := range objects {
		content.WriteString("---\n")
		bytes, err := yaml.Marshal(o.Object)
		if err != nil {
			return "", err
		}
		content.Write(bytes)
	}
	return content.String(), nil
}

func toJSON(objects []*unstructured.Unstructured) (string, error) {
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("List"))
	for _, obj := range objects {
		uObj, err := toUnstructured(obj)
		if err != nil {
			return "", err
		}
		uList.Items = append(uList.Items, *uObj)
	}
	content, err := json.MarshalIndent(uList, "", "\t")
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func rmBadFields(u *unstructured.Unstructured) {
	// The conversion to unstructured automatically fills these in.
	delete(u.Object, "status")
	delete(u.Object["metadata"].(map[string]interface{}), "creationTimestamp")
}
