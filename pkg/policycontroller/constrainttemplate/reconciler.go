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

package constrainttemplate

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/policycontroller/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type constraintTemplateReconciler struct {
	client client.Client
}

func newReconciler(cl client.Client) *constraintTemplateReconciler {
	return &constraintTemplateReconciler{cl}
}

// Reconcile handles Requests from the ConstraintTemplate controller. It will
// annotate ConstraintTemplates based upon their status.
func (c *constraintTemplateReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	ct := emptyConstraintTemplate()
	if err := c.client.Get(ctx, request.NamespacedName, &ct); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("Error getting ConstraintTemplate %q: %v", request.NamespacedName, err)
			return reconcile.Result{}, err
		}

		klog.Infof("ConstraintTemplate %q was deleted", request.NamespacedName)
		return reconcile.Result{}, nil
	}

	ctCopy := ct.DeepCopy()
	patch := client.MergeFrom(ctCopy)
	annotateConstraintTemplate(ct)

	if !util.AnnotationsChanged(&ct, ctCopy) {
		klog.V(3).Infof("ConstraintTemplate %q was upserted, but annotations are unchanged", request.NamespacedName)
		return reconcile.Result{}, nil
	}

	klog.Infof("ConstraintTemplate %q was upserted", request.NamespacedName)
	err := c.client.Patch(ctx, &ct, patch)
	if err != nil {
		klog.Errorf("Failed to patch annotations for ConstraintTemplate: %v", err)
	}
	return reconcile.Result{}, err
}

// The following structs allow the code to deserialize Gatekeeper types without
// importing them as a direct dependency.
// These types are from templates.gatekeeper.sh/v1beta1

type constraintTemplateStatus struct {
	Created bool          `json:"created,omitempty"`
	ByPod   []byPodStatus `json:"byPod,omitempty"`
}

type byPodStatus struct {
	ID                 string                       `json:"id,omitempty"`
	ObservedGeneration int64                        `json:"observedGeneration,omitempty"`
	Errors             []util.PolicyControllerError `json:"errors,omitempty"`
}

func unmarshalCT(ct unstructured.Unstructured) (*constraintTemplateStatus, error) {
	status := &constraintTemplateStatus{}
	if err := util.UnmarshalStatus(ct, status); err != nil {
		return nil, err
	}
	return status, nil
}

// end Gatekeeper types

// annotateConstraintTemplate processes the given ConstraintTemplate and sets
// Nomos resource status annotations for it.
func annotateConstraintTemplate(ct unstructured.Unstructured) {
	util.ResetAnnotations(&ct)
	gen := ct.GetGeneration()

	status, err := unmarshalCT(ct)
	if err != nil {
		klog.Errorf("Failed to unmarshal ConstraintTemplate %q: %v", ct.GetName(), err)
		return
	}

	if status == nil || !status.Created {
		util.AnnotateReconciling(&ct, "ConstraintTemplate has not been created")
		return
	}

	if len(status.ByPod) == 0 {
		util.AnnotateReconciling(&ct, "ConstraintTemplate has not been processed by PolicyController")
		return
	}

	var reconcilingMsgs []string
	var errorMsgs []string
	for _, bps := range status.ByPod {
		if bps.ObservedGeneration != gen {
			reconcilingMsgs = append(reconcilingMsgs, fmt.Sprintf("[%s] PolicyController has an outdated version of ConstraintTemplate", bps.ID))
		} else {
			// We only look for errors if the version is up-to-date.
			statusErrs := util.FormatErrors(bps.ID, bps.Errors)
			errorMsgs = append(errorMsgs, statusErrs...)
		}
	}

	if len(reconcilingMsgs) > 0 {
		util.AnnotateReconciling(&ct, reconcilingMsgs...)
	}
	if len(errorMsgs) > 0 {
		util.AnnotateErrors(&ct, errorMsgs...)
	}
}
