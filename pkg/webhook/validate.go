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
	"context"
	"fmt"

	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// AddValidator adds the admission webhook validator to the passed manager.
func AddValidator(mgr manager.Manager) error {
	handler, err := handler(mgr.GetConfig())
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register(configuration.ServingPath, &webhook.Admission{
		Handler: handler,
	})
	return nil
}

// Validator is the part of the validating webhook which handles admission
// requests and admits or denies them.
type Validator struct {
	differ *ObjectDiffer
}

var _ admission.Handler = &Validator{}

// Handler returns a Validator which satisfies the admission.Handler interface.
func handler(cfg *rest.Config) (*Validator, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	vc, err := declared.NewValueConverter(dc)
	if err != nil {
		return nil, err
	}
	return &Validator{&ObjectDiffer{vc}}, nil
}

// Handle implements admission.Handler
func (v *Validator) Handle(_ context.Context, req admission.Request) admission.Response {
	// An admission request for a sub-resource (such as a Scale) will not include
	// the full parent for us to validate until the admission chain is fixed:
	// https://github.com/kubernetes/enhancements/pull/1600
	// Until then, we will not configure the webhook to intercept subresources so
	// this block should never be reached.
	if req.SubResource != "" {
		klog.Errorf("Unable to review admission request for sub-resource: %v", req)
		return allow()
	}

	username := req.UserInfo.Username
	// Check UserInfo for Config Sync service account and handle if found.
	if isConfigSyncSA(req.UserInfo) {
		if isImporter(username) {
			// Config Sync importer can always update a resource.
			return allow()
		}
		// Perform manager precedence check to verify this Config Sync reconciler
		// can manage the object.
		mgr, oldObj, err := objectManager(req)
		if err != nil {
			klog.Error(err.Error())
			return allow()
		}
		if canManage(username, mgr) {
			return allow()
		}
		klog.Errorf("%s can not manage object %q which is already managed by %s", username, core.GKNN(oldObj), mgr)
		return deny(metav1.StatusReasonUnauthorized, fmt.Sprintf("%s can not manage object %q which is already managed by %s", username, core.GKNN(oldObj), mgr))
	}

	// Handle the requests for ResourceGroup CRs.
	if isResourceGroupRequest(req) {
		return handleResourceGroupRequest(req)
	}

	// Convert to client.Objects for convenience.
	oldObj, newObj, err := convertObjects(req)
	if err != nil {
		klog.Error(err.Error())
		return allow()
	}

	switch req.Operation {
	case admissionv1.Create:
		return v.handleCreate(newObj, username)
	case admissionv1.Delete:
		return v.handleDelete(oldObj, username)
	case admissionv1.Update:
		return v.handleUpdate(oldObj, newObj, username)
	default:
		klog.Errorf("Unsupported operation: %v from %s", req.Operation, username)
		return allow()
	}
}

func (v *Validator) handleCreate(newObj client.Object, username string) admission.Response {
	if differ.ManagedByConfigSync(newObj) {
		klog.Errorf("%s is not authorized to create managed resource %q", username, core.GKNN(newObj))
		return deny(metav1.StatusReasonUnauthorized, fmt.Sprintf("%s is not authorized to create managed resource %q", username, core.GKNN(newObj)))
	}
	return allow()
}

func (v *Validator) handleDelete(oldObj client.Object, username string) admission.Response {
	// This means a delete request was previously made and accepted, but removal of the API object is not yet complete.
	// See http://b/199235728#comment16 for more details.
	if oldObj.GetDeletionTimestamp() != nil {
		return allow()
	}
	if differ.ManagedByConfigSync(oldObj) {
		klog.Errorf("%s is not authorized to delete managed resource %q", username, core.GKNN(oldObj))
		return deny(metav1.StatusReasonUnauthorized, fmt.Sprintf("%s is not authorized to delete managed resource %q", username, core.GKNN(oldObj)))
	}
	return allow()
}

func (v *Validator) handleUpdate(oldObj, newObj client.Object, username string) admission.Response {
	if !differ.ManagedByConfigSync(oldObj) && !differ.ManagedByConfigSync(newObj) {
		// Both oldObj and newObj are not managed by Config Sync.
		// The webhook should be configured to only intercept resources which are
		// managed by Config Sync.
		klog.Warningf("Received admission request from %s for unmanaged object %q", username, core.GKNN(newObj))
		return allow()
	}

	// Build a diff set between old and new objects.
	diffSet, err := v.differ.FieldDiff(oldObj, newObj)
	if err != nil {
		klog.Errorf("Failed to generate field diff set for object %q: %v", core.GKNN(oldObj), err)
		return allow()
	}

	// If the diff set includes any ConfigSync labels or annotations, reject the
	// request immediately.
	if csSet := ConfigSyncMetadata(diffSet); !csSet.Empty() {
		klog.Errorf("%s cannot modify Config Sync metadata of object %q: %s", username, core.GKNN(oldObj), csSet.String())
		return deny(metav1.StatusReasonForbidden, fmt.Sprintf("%s cannot modify Config Sync metadata of object %q: %s", username, core.GKNN(oldObj), csSet.String()))
	}

	if oldObj.GetAnnotations()[csmetadata.LifecycleMutationAnnotation] == csmetadata.IgnoreMutation {
		// We ignore user modifications to this resource. Per the above check, we
		// know that this annotation has not been altered.
		return allow()
	}

	// Use the ConfigSync declared fields annotation to build the set of fields
	// which should not be modified.
	declaredSet, err := DeclaredFields(oldObj)
	if err != nil {
		klog.Errorf("Failed to decoded declared fields for object %q: %v", core.GKNN(oldObj), err)
		return allow()
	}

	// If the diff set and declared set have any fields in common, reject the
	// request. Otherwise allow it.
	invalidSet := diffSet.Intersection(declaredSet)
	if !invalidSet.Empty() {
		klog.Errorf("%s cannot modify fields of object %q managed by Config Sync: %s", username, core.GKNN(oldObj), invalidSet.String())
		return deny(metav1.StatusReasonForbidden, fmt.Sprintf("%s cannot modify fields of object %q managed by Config Sync: %s", username, core.GKNN(oldObj), invalidSet.String()))
	}
	return allow()
}

func convertObjects(req admission.Request) (client.Object, client.Object, error) {
	var oldObj client.Object
	switch {
	case req.OldObject.Object != nil:
		// We got an already-parsed object.
		var ok bool
		oldObj, ok = req.OldObject.Object.(client.Object)
		if !ok {
			return nil, nil, fmt.Errorf("failed to convert to client.Object: %v", req.OldObject.Object)
		}
	case req.OldObject.Raw != nil:
		// We got raw JSON bytes instead of an object.
		oldU := &unstructured.Unstructured{}
		if err := oldU.UnmarshalJSON(req.OldObject.Raw); err != nil {
			return nil, nil, errors.Wrapf(err, "failed to convert to client.Object: %v", req.OldObject.Raw)
		}
		oldObj = oldU
	}

	var newObj client.Object
	switch {
	case req.Object.Object != nil:
		// We got an already-parsed object.
		var ok bool
		newObj, ok = req.Object.Object.(client.Object)
		if !ok {
			return nil, nil, fmt.Errorf("failed to convert to client.Object: %v", req.Object.Object)
		}
	case req.Object.Raw != nil:
		// We got raw JSON bytes instead of an object.
		newU := &unstructured.Unstructured{}
		if err := newU.UnmarshalJSON(req.Object.Raw); err != nil {
			return nil, nil, errors.Wrapf(err, "failed to convert to client.Object: %v", req.Object.Raw)
		}
		newObj = newU
	}
	return oldObj, newObj, nil
}

func objectManager(req admission.Request) (string, client.Object, error) {
	oldObj, newObj, err := convertObjects(req)
	if err != nil {
		return "", nil, err
	}
	mgr := getManager(oldObj)
	if mgr == "" {
		mgr = getManager(newObj)
	}
	return mgr, oldObj, nil
}

func getManager(obj client.Object) string {
	if obj == nil {
		return ""
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[csmetadata.ResourceManagerKey]
}

func allow() admission.Response {
	return admission.Allowed("")
}

func deny(reason metav1.StatusReason, message string) admission.Response {
	resp := admission.Denied(string(reason))
	resp.Result.Message = message
	return resp
}
