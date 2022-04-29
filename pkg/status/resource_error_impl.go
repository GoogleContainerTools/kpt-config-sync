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

package status

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type resourceErrorImpl struct {
	underlying Error
	resources  []client.Object
}

var _ ResourceError = resourceErrorImpl{}

// Error implements error.
func (r resourceErrorImpl) Error() string {
	return format(r)
}

// Is implements Error.
func (r resourceErrorImpl) Is(target error) bool {
	return r.underlying.Is(target)
}

// Code implements Error.
func (r resourceErrorImpl) Code() string {
	return r.underlying.Code()
}

// Body implements Error.
func (r resourceErrorImpl) Body() string {
	return formatBody(r.underlying.Body(), "\n\n", formatResources(r.resources...))
}

// Errors implements MultiError.
func (r resourceErrorImpl) Errors() []Error {
	return []Error{r}
}

// Resources implements ResourceError.
func (r resourceErrorImpl) Resources() []client.Object {
	return r.resources
}

// ToCME implements Error.
func (r resourceErrorImpl) ToCME() v1.ConfigManagementError {
	return fromResourceError(r)
}

// ToCSE implements Error.
func (r resourceErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	return cseFromResourceError(r)
}

// Cause implements causer.
func (r resourceErrorImpl) Cause() error {
	return r.underlying.Cause()
}

// formatResources returns a formatted string containing all Resources in the ResourceError.
func formatResources(resources ...client.Object) string {
	resStrs := make([]string, len(resources))
	for i, res := range resources {
		resStrs[i] = PrintResource(res)
	}
	// Sort to ensure deterministic resource printing order.
	sort.Strings(resStrs)
	return strings.Join(resStrs, "\n\n")
}

// GetSourceAnnotation returns the string value of the SourcePath Annotation.
// Returns empty string if unset or the object has no annotations.
func GetSourceAnnotation(obj client.Object) string {
	as := obj.GetAnnotations()
	if as == nil {
		return ""
	}
	return as[metadata.SourcePathAnnotationKey]
}

// PrintResource returns a human-readable output for the Resource.
func PrintResource(r client.Object) string {
	var sb strings.Builder
	if sourcePath := GetSourceAnnotation(r); sourcePath != "" {
		sb.WriteString(fmt.Sprintf("source: %s\n", sourcePath))
	}
	if r.GetNamespace() != "" {
		sb.WriteString(fmt.Sprintf("namespace: %s\n", r.GetNamespace()))
	}
	sb.WriteString(fmt.Sprintf("metadata.name:%s\n", name(r)))
	sb.WriteString(printGroupVersionKind(r.GetObjectKind().GroupVersionKind()))
	return sb.String()
}

// name returns the empty string if r.Name is the empty string, otherwise prepends a space.
func name(r client.Object) string {
	if r.GetName() == "" {
		return ""
	}
	return " " + r.GetName()
}

// printGroupVersionKind returns a human-readable output for the GroupVersionKind.
func printGroupVersionKind(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf(
		"group:%[1]s\n"+
			"version: %[2]s\n"+
			"kind: %[3]s",
		group(gvk.GroupKind()), gvk.Version, gvk.Kind)
}

// group returns the empty string if gvk.Group is the empty string, otherwise prepends a space.
func group(gk schema.GroupKind) string {
	if gk.Group == "" {
		// Avoid unsightly whitespace if group is the empty string.
		return ""
	}
	// Prepends space to separate it from "group:"
	return " " + gk.Group
}
