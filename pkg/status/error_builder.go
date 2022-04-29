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
	"strings"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/importer/id"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrorBuilders handle the oft-duplicated logic we use for generating error messages.
//
// Each Nomos Error has a unique code, "KNV" followed by four digits. Errors with the same KNV code
// share a strong unifying feature (e.g. they result from an illegal annotation), but may include
// variations (e.g. different illegal annotations). If you would use essentially the same
// explanation and suggest the same fix for the problem, reuse the ErrorBuilder for that code. The
// four digits of an error code have no  meaning except:
// - 1XXX, the user has a mistake in their repository they need to fix.
// - 2XXX, something went wrong in the cluster - it could be transient or users may need to change something on the cluster.
// - 9998, InternalError, and
// - 9999, UndocumentedError.
//
// Construct a new ErrorBuilder by passing in a code to NewErrorBuilder. If the code is not unique,
// the code will panic when packages are loaded. This ensures the code cannot run at all if there
// are duplicate error codes. To see the list of currently used error codes, run
// `go run cmd/nomoserrors/main.go`.
//
// var myErrorBuilder = NewErrorBuilder("1234").Sprint("a coloring problem")
//
//
// Libraries should not directly expose ErrorBuilders, but keep them package private and instead
// provide functions that tell callers the correct number and position of formatting arguments. This
// ensures Error message consistency for a given KNV, as the set of methods using that ErrorBuilder
// is confined to a single package (and so is discoverable). If possible, all methods using an
// ErrorBuilder should be in the same file.
//
// func MyError(color string, count int) Error {
//   return myErrorBuilder.Sprintf("problem with color %q when count is %d", color, count).Build()
// }
//
//
// Part of the benefit of having structured Errors is that Errors can advertise the particular
// structure they have. For example, ResourceError pertains to a set of Resources. ErrorBuilders
// automatically take care of the boilerplate of setting this up, so other parts of our
// infrastructure can easily report errors to users (and machines) in structured manners.
//
// func MyResourceError(color string, left client.Object, right client.Object) ResourceError {
//   return myErrorBuilder.Sprintf("expected both resources to be colored %q", color).
//     BuildWithResources(left, right)
// }
//
//
// For now ErrorBuilders do **not** support constructing structured errors that report
// 1) both paths and resources
// 2) multiple sets of paths/resources
// This is not by design, but could be implemented in the future. For now we haven't run into
// compelling use cases, but it is easy to imagine this may be necessary in the future.
//

// ErrorBuilder constructs complex, structured error messages.
// Use NewErrorBuilder to register a KNV for a new code.
type ErrorBuilder struct {
	error Error
}

// NewErrorBuilder returns an ErrorBuilder that can be used to generate errors. Registers this
// call with the passed unique code. Panics if there is an error code collision.
func NewErrorBuilder(code string) ErrorBuilder {
	register(code)
	return ErrorBuilder{error: baseErrorImpl{
		code: code,
	}}
}

// Build returns the Error inside the ErrorBuilder.
func (eb ErrorBuilder) Build() Error {
	return eb.error
}

// BuildWithPaths adds paths within the repository to the Error.
func (eb ErrorBuilder) BuildWithPaths(paths ...id.Path) PathError {
	if len(paths) == 0 {
		return nil
	}
	return pathErrorImpl{
		underlying: eb.error,
		paths:      paths,
	}
}

// BuildWithResources adds resources declared in the repository to the Error.
func (eb ErrorBuilder) BuildWithResources(resources ...client.Object) ResourceError {
	if len(resources) == 0 {
		return nil
	}
	return resourceErrorImpl{
		underlying: eb.error,
		resources:  resources,
	}
}

// BuildWithConflictingManagers adds the conflicting manager and the resource declared in the repository to the Error.
// newManager is the manager annotation for the current remediator/reconciler.
// currentManager is the manager annotation in the actual resource. It is also known as conflictingManager.
func (eb ErrorBuilder) BuildWithConflictingManagers(resource client.Object, newManager, currentManager string) ManagementConflictError {
	return managementConflictErrorImpl{
		underlying:     eb.error,
		resource:       resource,
		newManager:     newManager,
		currentManager: currentManager,
	}
}

// Sprint adds a message string into the Error inside the ErrorBuilder.
func (eb ErrorBuilder) Sprint(message string) ErrorBuilder {
	return ErrorBuilder{error: messageErrorImpl{
		underlying: eb.error,
		message:    message,
	}}
}

// Sprintf adds a formatted string into the Error inside the ErrorBuilder.
func (eb ErrorBuilder) Sprintf(format string, a ...interface{}) ErrorBuilder {
	for _, e := range a {
		if _, isError := e.(error); isError {
			// Don't format errors in string form because we lose type information;
			// use .Wrap instead.
			reportMisuse("attempted format error when .Wrap should have been used")
		}
	}

	message := fmt.Sprintf(format, a...)
	if strings.Contains(message, "%!") {
		// Make sure there aren't string formatting errors in the error message.
		// Don't replace the below with string formatting syntax or it may cause
		// a stack overflow.
		reportMisuse("improperly formatted error message: " + message)
	}
	return ErrorBuilder{error: messageErrorImpl{
		underlying: eb.error,
		message:    message,
	}}
}

// Wrap adds an error into the Error inside the ErrorBuilder.
func (eb ErrorBuilder) Wrap(toWrap error) ErrorBuilder {
	if e, isStatusError := toWrap.(Error); isStatusError {
		// We don't allow wrapping KNV errors in other KNV errors.
		klog.Info(e.Code())
		reportMisuse("attempted wrap a status.Error in another status.Error")
	}
	if toWrap == nil {
		return ErrorBuilder{error: nil}
	}
	return ErrorBuilder{error: wrappedErrorImpl{
		underlying: eb.error,
		wrapped:    toWrap,
	}}
}
