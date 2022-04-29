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
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/importer/id"
	"sigs.k8s.io/cli-utils/pkg/multierror"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const urlBase = "For more information, see https://g.co/cloud/acm-errors#knv"

func url(code string) string {
	return urlBase + code
}

func knv(id string) string {
	return fmt.Sprintf("KNV%s", id)
}

func prefix(code string) string {
	return fmt.Sprintf("%s: ", code)
}

// Error defines a Kubernetes Nomos Vet error
// These are GKE Config Management directory errors which are shown to the user and documented.
type Error interface {
	causer
	MultiError
	// ToCME converts the implementor into ConfigManagementError, preserving
	// structured information.
	ToCME() v1.ConfigManagementError
	// ToCSE converts the implementor into ConfigSyncError, preserving structured
	// information.
	ToCSE() v1beta1.ConfigSyncError
	// Code is the unique identifier of the error to help users find documentation.
	Code() string
	// Body is the body of the error to be printed.
	Body() string
	// Is allows comparing error types through errors.Is.
	Is(target error) bool
}

// causer defines an error with an underlying cause.
type causer interface {
	Cause() error
}

// registered is a map from error codes to instances of the types they represent.
// Entries set to true are reserved and MUST NOT be reused.
var registered = map[string]bool{
	"1000": true,
	"1001": true,
	"1002": true,
	"1008": true,
	"1012": true,
	"1015": true,
	"1016": true,
	"1018": true,
	"1022": true,
	"1023": true,
	"1024": true,
	"1025": true,
	"1026": true,
	"1035": true,
	"1037": true,
	"1040": true,
	"1049": true,
	"1051": true,
	"1059": true,
	"1062": true,
	"1063": true,
}

// format formats error messages consistently.
func format(err Error) string {
	var sb strings.Builder
	sb.WriteString(prefix(knv(err.Code())))
	sb.WriteString(formatCyclicDepErr(err.Body()))
	sb.WriteString("\n\n")
	sb.WriteString(url(err.Code()))
	return sb.String()
}

func formatBody(message, separator, context string) string {
	var sb strings.Builder
	sb.WriteString(message)
	if context != "" {
		sb.WriteString(separator)
		sb.WriteString(context)
	}
	return sb.String()
}

// formatCyclicDepErr ensures that we strip newlines and replace them with
// ";" to ensure readability while maintaining ease for log parsing.
func formatCyclicDepErr(message string) string {
	if !strings.Contains(message, "cyclic dependency:") {
		return message
	}

	msgSplit := strings.Split(message, "\n"+multierror.Prefix)

	return fmt.Sprintf("%s %s", msgSplit[0], strings.Join(msgSplit[1:], "; "))

}

// PathError defines a status error associated with one or more path-identifiable locations in the
// repo.
type PathError interface {
	Error
	RelativePaths() []id.Path
}

func nextCandidate(code string) (int, error) {
	c, err := strconv.Atoi(code)
	if err != nil {
		return 0, err
	}

	for ; true; c++ {
		if _, found := registered[strconv.Itoa(c)]; found {
			continue
		}
		return c, nil
	}
	panic("unreachable code")
}

// Register marks the passed error code as used. err is a sample value of Error
// for this code.
func register(code string) {
	if _, exists := registered[code]; exists {
		if c, err2 := nextCandidate(code); err2 == nil {
			reportMisuse(fmt.Sprintf("duplicate error code %s, next candidate: %d", code, c))
		} else {
			reportMisuse(fmt.Sprintf("duplicate error code %s", code))
		}
	}
	registered[code] = true
}

// CodeRegistry returns a sorted list of currently registered error codes.
func CodeRegistry() []string {
	var codes []string
	for code := range registered {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	return codes
}

// toErrorResource converts a Resource into a v1.ErrorResource.
func toErrorResource(r client.Object) v1.ErrorResource {
	return v1.ErrorResource{
		SourcePath:        GetSourceAnnotation(r),
		ResourceName:      r.GetName(),
		ResourceNamespace: r.GetNamespace(),
		ResourceGVK:       r.GetObjectKind().GroupVersionKind(),
	}
}

// FromError embeds the error message and error code into a ConfigManagementError.
func fromError(err Error) v1.ConfigManagementError {
	return v1.ConfigManagementError{
		ErrorMessage: err.Error(),
		Code:         knv(err.Code()),
	}
}

// FromPathError converts a PathError to a ConfigManagementError.
func fromPathError(err PathError) v1.ConfigManagementError {
	cme := fromError(err)
	for _, path := range err.RelativePaths() {
		cme.ErrorResources = append(
			cme.ErrorResources,
			v1.ErrorResource{SourcePath: path.SlashPath()})
	}
	return cme
}

// FromResourceError converts a ResourceError to a ConfigManagementError.
func fromResourceError(err ResourceError) v1.ConfigManagementError {
	cme := fromError(err)
	for _, r := range err.Resources() {
		cme.ErrorResources = append(cme.ErrorResources, toErrorResource(r))
	}
	return cme
}

func toResourceRef(r client.Object) v1beta1.ResourceRef {
	gvk := r.GetObjectKind().GroupVersionKind()
	return v1beta1.ResourceRef{
		SourcePath: GetSourceAnnotation(r),
		Name:       r.GetName(),
		Namespace:  r.GetNamespace(),
		GVK: metav1.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
		},
	}
}

func cseFromError(err Error) v1beta1.ConfigSyncError {
	return v1beta1.ConfigSyncError{
		Code:         err.Code(),
		ErrorMessage: err.Error(),
	}
}

func cseFromPathError(err PathError) v1beta1.ConfigSyncError {
	cse := cseFromError(err)
	for _, path := range err.RelativePaths() {
		cse.Resources = append(cse.Resources, v1beta1.ResourceRef{
			SourcePath: path.SlashPath(),
		})
	}
	return cse
}

func cseFromResourceError(err ResourceError) v1beta1.ConfigSyncError {
	cse := cseFromError(err)
	for _, r := range err.Resources() {
		cse.Resources = append(cse.Resources, toResourceRef(r))
	}
	return cse
}
