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
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
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
			switch err.(type) {
			case status.MultiError:
				// This error is from the function setDefaultProtocol.
				// No schema checking involved.
				errs = status.Append(errs, err)
			default:
				errs = status.Append(errs, status.EncodeDeclaredFieldError(obj.Unstructured, err))
				// This error could be due to an out of date schema.
				// So the converter needs to be refreshed.
				needRefresh = true
			}
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
	u, isUnstructured := obj.(*unstructured.Unstructured)
	if isUnstructured {
		err = setDefaultProtocol(u)
		if err != nil {
			return nil, err
		}
	}

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

// setDefaultProtocol sets the nested protocol field in anything containing
// an array of Ports.
// TODO: This should be deleted once we've upgraded to k8s 1.21 libraries.
func setDefaultProtocol(u *unstructured.Unstructured) status.MultiError {
	var errs []error
	switch u.GroupVersionKind().GroupKind() {
	case kinds.Pod().GroupKind():
		errs = setDefaultProtocolInNestedPodSpec(u.Object, "spec")
	case kinds.DaemonSet().GroupKind(),
		kinds.Deployment().GroupKind(),
		kinds.ReplicaSet().GroupKind(),
		kinds.StatefulSet().GroupKind(),
		kinds.Job().GroupKind(),
		kinds.ReplicationController().GroupKind():
		errs = setDefaultProtocolInNestedPodSpec(u.Object, "spec", "template", "spec")
	case kinds.CronJob().GroupKind():
		errs = setDefaultProtocolInNestedPodSpec(u.Object, "spec", "jobTemplate", "spec", "template", "spec")
	case kinds.Service().GroupKind():
		errs = setDefaultProtocolInNestedPorts(u.Object, true, "spec", "ports")
	}

	if len(errs) > 0 {
		// These errors represent malformed objects. The user needs to correct their
		// YAML/JSON as it is invalid. In almost all cases these errors are caught
		// before here, but we still need to handle the errors rather than ignoring
		// them. So this is _necessary_, but it doesn't need to be perfect. If in
		// practice these errors come up more frequently we'll need to revisit.
		message := ""
		for _, err := range errs {
			message += err.Error() + "\n"
		}
		return status.ObjectParseError(u, errors.New(message))
	}

	return nil
}

func setDefaultProtocolInNestedPodSpec(obj map[string]interface{}, fields ...string) []error {
	// We have to use the generic NestedFieldNoCopy and manually cast to a map as unstructured.NestedMap
	// returns a deepcopy of the object, which does not allow us to modify the object in place.
	podSpec, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil {
		return []error{fmt.Errorf("unable to get pod spec: %w", err)}
	}
	if !found || podSpec == nil {
		return []error{fmt.Errorf(".%s is required", strings.Join(fields, "."))}
	}

	mPodSpec, ok := podSpec.(map[string]interface{})
	if !ok {
		return []error{fmt.Errorf(".%s accessor error: %v is of the type %T, expected map[string]interface{}", strings.Join(fields, "."), podSpec, podSpec)}
	}

	return setDefaultProtocolInPodSpec(mPodSpec, fields)
}

func setDefaultProtocolInPodSpec(podSpec map[string]interface{}, fields []string) []error {
	var errs []error

	// Use the more generic NestedField instead of NestedSlice. We can have occurences where
	// the nested slice is empty/nill/null in the resource, causing unstructured.NestedSlice to
	// error when it tries to assert nil to be []interface{}. We need to be able to ignore empty
	// initContainers by handling nil values.
	initContainers, found, err := unstructured.NestedFieldNoCopy(podSpec, "initContainers")
	if err != nil {
		errs = append(errs, err)
	} else if found && initContainers != nil {
		initContainersSlice, ok := initContainers.([]interface{})
		if !ok {
			errs = append(errs, fmt.Errorf(".%s.initContainers accessor error: %v is of the type %T, expected []interface{}", strings.Join(fields, "."), initContainers, initContainers))
		} else {
			errs = updateDefaultProtocolInContainers(podSpec, initContainersSlice, "initContainers", errs)
		}
	}

	// We don't need to use the generic NestedField function since we want it to error
	// if the containers field is empty. A pod spec with no containers field is invalid.
	containers, found, err := unstructured.NestedSlice(podSpec, "containers")
	if err != nil {
		errs = append(errs, err)
	} else if found {
		errs = updateDefaultProtocolInContainers(podSpec, containers, "containers", errs)
	}

	return errs
}

func updateDefaultProtocolInContainers(podSpec map[string]interface{}, containers []interface{}, field string, errs []error) []error {
	setErrs := setDefaultProtocolInContainers(containers)
	if len(setErrs) != 0 {
		return append(errs, setErrs...)
	}

	err := unstructured.SetNestedSlice(podSpec, containers, field)
	if err != nil {
		return append(errs, err)
	}

	return errs
}

func setDefaultProtocolInContainers(containers []interface{}) []error {
	var errs []error
	for _, c := range containers {
		setErrs := setDefaultProtocolInContainer(c)
		if len(setErrs) > 0 {
			errs = append(errs, setErrs...)
		}
	}
	return errs
}

func setDefaultProtocolInContainer(container interface{}) []error {
	mContainer, ok := container.(map[string]interface{})
	if !ok {
		return []error{errors.New("container must be a map")}
	}

	return setDefaultProtocolInNestedPorts(mContainer, false, "ports")
}

func setDefaultProtocolInNestedPorts(obj map[string]interface{}, mustExist bool, fields ...string) []error {
	ports, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil {
		return []error{err}
	}
	if !found || ports == nil {
		// Service resource requires the port field to be specified, or it is not a valid resource.
		if mustExist {
			return []error{fmt.Errorf(".%s is required", strings.Join(fields, "."))}
		}
		// Other resources can have empty ports field, and we can gracefully return early.
		return nil
	}

	sPorts, ok := ports.([]interface{})
	if !ok {
		return []error{fmt.Errorf(".%s accessor error: %v is of the type %T, expected []interface{}", strings.Join(fields, "."), ports, ports)}
	}

	setErrs := setDefaultProtocolInPorts(sPorts)
	if len(setErrs) != 0 {
		return setErrs
	}

	err = unstructured.SetNestedSlice(obj, sPorts, fields...)
	if err != nil {
		return []error{err}
	}
	return nil
}

func setDefaultProtocolInPorts(ports []interface{}) []error {
	var errs []error
	for _, p := range ports {
		err := setDefaultProtocolInPort(p)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func setDefaultProtocolInPort(port interface{}) error {
	mPort, ok := port.(map[string]interface{})
	if !ok {
		return errors.New("port must be a map")
	}

	if _, found := mPort["protocol"]; !found {
		mPort["protocol"] = string(corev1.ProtocolTCP)
	}
	return nil
}
