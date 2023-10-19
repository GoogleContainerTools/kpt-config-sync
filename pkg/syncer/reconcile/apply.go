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

package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/openapi"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	m "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/cache"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const noOpPatch = "{}"
const rmCreationTimestampPatch = "{\"metadata\":{\"creationTimestamp\":null}}"

// Applier updates a resource from its current state to its intended state using apply operations.
type Applier interface {
	Create(ctx context.Context, obj *unstructured.Unstructured) (string, status.Error)
	Update(ctx context.Context, intendedState, currentState *unstructured.Unstructured) (string, status.Error)
	// RemoveNomosMeta performs a PUT (rather than a PATCH) to ensure that labels and annotations are removed.
	RemoveNomosMeta(ctx context.Context, intent *unstructured.Unstructured, controller string) (string, status.Error)
	Delete(ctx context.Context, obj *unstructured.Unstructured) (string, status.Error)
	GetClient() client.Client
	ResolveFight(time.Time, client.Object) (bool, status.Error)
}

// clientApplier does apply operations on resources, client-side, using the same approach as running `kubectl apply`.
type clientApplier struct {
	dynamicClient    dynamic.Interface
	discoveryClient  discovery.DiscoveryInterface
	openAPIResources openapi.Resources
	client           *syncerclient.Client
	fights           fight.Detector
}

var _ Applier = &clientApplier{}

// NewApplierForMultiRepo returns a new clientApplier for callers with multi repo feature enabled.
func NewApplierForMultiRepo(cfg *rest.Config, client *syncerclient.Client) (Applier, error) {
	return newApplier(cfg, client)
}

func newApplier(cfg *rest.Config, client *syncerclient.Client) (Applier, error) {
	c, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	oa, err := openapi.NewOpenAPIParser(dc).Parse()
	if err != nil {
		return nil, err
	}

	return &clientApplier{
		dynamicClient:    c,
		discoveryClient:  dc,
		openAPIResources: oa,
		client:           client,
		fights:           fight.NewDetector(),
	}, nil
}

// Create implements Applier.
func (c *clientApplier) Create(ctx context.Context, intendedState *unstructured.Unstructured) (string, status.Error) {
	var err status.Error
	// APIService is handled specially by client-side apply due to
	// https://github.com/kubernetes/kubernetes/issues/89264
	if intendedState.GroupVersionKind().GroupKind() == kinds.APIService().GroupKind() {
		err = c.create(ctx, intendedState)
	} else {
		if err1 := c.client.Patch(ctx, intendedState, client.Apply, client.FieldOwner(configsync.FieldManager)); err1 != nil {
			err = status.ResourceWrap(err1, "unable to apply resource", intendedState)
		}
	}
	metrics.Operations.WithLabelValues("create", metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, m.RemediatorController, "create", m.StatusTagKey(err))

	if err != nil {
		klog.V(3).Infof("Failed to create object %v: %v", core.GKNN(intendedState), err)
		return "", err
	}
	logErr, err := c.fights.DetectFight(time.Now(), intendedState)
	if logErr {
		klog.Errorf("Fight detected on create of %s.", description(intendedState))
	}
	if err == nil {
		klog.V(3).Infof("Created object %v", core.GKNN(intendedState))
	}
	return intendedState.GetResourceVersion(), err
}

// Update implements Applier.
func (c *clientApplier) Update(ctx context.Context, intendedState, currentState *unstructured.Unstructured) (string, status.Error) {
	patch, err := c.update(ctx, intendedState, currentState)
	metrics.Operations.WithLabelValues("update", metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, m.RemediatorController, "update", m.StatusTagKey(err))

	switch {
	case apierrors.IsConflict(err):
		return "", syncerclient.ConflictUpdateOldVersion(err, intendedState)
	case apierrors.IsNotFound(err):
		return "", syncerclient.ConflictUpdateDoesNotExist(err, intendedState)
	case err != nil:
		return "", status.ResourceWrap(err, "unable to update resource", intendedState)
	}

	updated := !isNoOpPatch(patch)
	if updated {
		logFight, err := c.fights.DetectFight(time.Now(), intendedState)
		if logFight {
			diff := cmp.Diff(currentState, intendedState)
			klog.Errorf("Fight detected on update of %s with difference %s", description(intendedState), diff)
		}
		if err == nil {
			klog.V(3).Infof("The object %v was updated with the patch %v", core.GKNN(currentState), string(patch))
		}
		return intendedState.GetResourceVersion(), err
	}

	klog.V(3).Infof("The object %v is up to date.", core.GKNN(currentState))
	return currentState.GetResourceVersion(), nil
}

// RemoveNomosMeta implements Applier.
func (c *clientApplier) RemoveNomosMeta(ctx context.Context, u *unstructured.Unstructured, controller string) (string, status.Error) {
	var changed bool
	_, err := c.client.Apply(ctx, u, func(obj client.Object) (client.Object, error) {
		changed = metadata.RemoveConfigSyncMetadata(obj)
		if !changed {
			return obj, syncerclient.NoUpdateNeeded()
		}
		return obj, nil
	})
	metrics.Operations.WithLabelValues("update", metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, controller, "update", m.StatusTagKey(err))

	if changed {
		klog.V(3).Infof("RemoveNomosMeta changed the object %v", core.GKNN(u))
	} else {
		klog.V(3).Infof("RemoveNomosMeta did not change the object %v", core.GKNN(u))
	}
	return u.GetResourceVersion(), err
}

// Delete implements Applier.
func (c *clientApplier) Delete(ctx context.Context, obj *unstructured.Unstructured) (string, status.Error) {
	err := c.client.Delete(ctx, obj)
	metrics.Operations.WithLabelValues("delete", metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, m.RemediatorController, "delete", m.StatusTagKey(err))

	if err != nil {
		klog.V(3).Infof("Failed to delete object %v: %v", core.GKNN(obj), err)
		return "", err
	}
	logFight, err := c.fights.DetectFight(time.Now(), obj)
	if logFight {
		klog.Errorf("Fight detected on delete of %s.", description(obj))
	}
	if err == nil {
		klog.V(3).Infof("Deleted object %v", core.GKNN(obj))
	}
	return cache.DeletedResourceVersion, err
}

// create creates the resource with the declared-config annotation set.
func (c *clientApplier) create(ctx context.Context, obj *unstructured.Unstructured) status.Error {
	// When multi-repo feature is enabled, use kubectl last-applied-annotation.
	err := util.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return status.ResourceWrap(err, "could not generate apply annotation on create", obj)
	}

	return c.client.Create(ctx, obj)
}

// clientFor returns the client which may interact with the passed object.
func (c *clientApplier) clientFor(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	apiResource, rErr := c.resource(gvk)
	if rErr != nil {
		return nil, errors.Wrapf(rErr, "unable to get resource client for %q", gvk.String())
	}

	gvr := gvk.GroupVersion().WithResource(apiResource)
	// If namespace is the empty string (as is the case for cluster-scoped resources), the
	// client correctly returns itself.
	return c.dynamicClient.Resource(gvr).Namespace(obj.GetNamespace()), nil
}

// apply updates a resource using the same approach as running `kubectl apply`.
func (c *clientApplier) update(ctx context.Context, intendedState, currentState *unstructured.Unstructured) ([]byte, error) {
	if intendedState.GroupVersionKind().GroupKind() == kinds.APIService().GroupKind() {
		return c.updateAPIService(ctx, intendedState, currentState)
	}
	objCopy := intendedState.DeepCopy()
	// Run the server-side apply dryrun first.
	// If the returned object doesn't change, skip running server-side apply.
	err := c.client.Patch(ctx, objCopy, client.Apply, client.FieldOwner(configsync.FieldManager), client.ForceOwnership, client.DryRunAll)
	if err != nil {
		return nil, err
	}
	if equal(objCopy, currentState) {
		return nil, nil
	}

	start := time.Now()
	err = c.client.Patch(ctx, intendedState, client.Apply, client.FieldOwner(configsync.FieldManager), client.ForceOwnership)
	duration := time.Since(start).Seconds()
	metrics.APICallDuration.WithLabelValues("update", metrics.StatusLabel(err)).Observe(duration)
	m.RecordAPICallDuration(ctx, "update", m.StatusTagKey(err), start)
	return []byte(cmp.Diff(currentState, intendedState)), err
}

// updateAPIService updates APIService type resources.
// APIService is handled specially by client-side apply due to
// https://github.com/kubernetes/kubernetes/issues/89264
func (c *clientApplier) updateAPIService(ctx context.Context, intendedState, currentState *unstructured.Unstructured) ([]byte, error) {
	resourceDescription := description(intendedState)
	// Serialize the current configuration of the object.
	current, cErr := runtime.Encode(unstructured.UnstructuredJSONScheme, currentState)
	if cErr != nil {
		return nil, errors.Errorf("could not serialize current configuration from %v", currentState)
	}

	var previous []byte
	var oErr error
	// Retrieve the last applied configuration of the object from the annotation.
	previous, oErr = util.GetOriginalConfiguration(currentState)
	if oErr != nil {
		return nil, errors.Errorf("could not retrieve original configuration from %v", currentState)
	}
	if previous == nil {
		klog.Warningf("3-way merge patch for %s may be incorrect due to missing last-applied-configuration annotation.", resourceDescription)
	}

	var modified []byte
	var mErr error

	modified, mErr = util.GetModifiedConfiguration(intendedState, true, unstructured.UnstructuredJSONScheme)
	if mErr != nil {
		return nil, errors.Errorf("could not serialize intended configuration from %v", intendedState)
	}

	resourceClient, rErr := c.clientFor(intendedState)
	if rErr != nil {
		return nil, nil
	}

	name := intendedState.GetName()
	gvk := intendedState.GroupVersionKind()
	//TODO: Add unit tests for patch return value.
	var err error

	// Attempt a strategic patch first.
	patch := c.calculateStrategic(resourceDescription, gvk, previous, modified, current)
	// If patch is nil, it means we don't have access to the schema.
	if patch != nil {
		err = attemptPatch(ctx, resourceClient, name, types.StrategicMergePatchType, patch)
		// UnsupportedMediaType error indicates an invalid strategic merge patch (always true for a
		// custom resource), so we reset the patch and try again below.
		if err != nil {
			if apierrors.IsUnsupportedMediaType(err) {
				patch = nil
			} else {
				klog.Warningf("strategic merge patch for %s failed: %v", resourceDescription, err)
			}
		}
	}

	// If we weren't able to do a Strategic Merge, we fall back to JSON Merge Patch.
	if patch == nil {
		patch, err = c.calculateJSONMerge(previous, modified, current)
		if err == nil {
			err = attemptPatch(ctx, resourceClient, name, types.MergePatchType, patch)
		}
	}

	if err != nil {
		// Don't wrap this error. We care about it's type information, and the
		// apierrors library doesn't properly recursively check for wrapped errors.
		return nil, err
	}

	if isNoOpPatch(patch) {
		klog.V(3).Infof("Ignoring no-op patch %s for %q", patch, resourceDescription)
	} else {
		klog.Infof("Patched %s", resourceDescription)
		klog.V(1).Infof("Patched with %s", patch)
	}

	return patch, nil
}

func (c *clientApplier) calculateStrategic(resourceDescription string, gvk schema.GroupVersionKind, previous, modified, current []byte) []byte {
	// Try to use schema from OpenAPI Spec if possible.
	gvkSchema := c.openAPIResources.LookupResource(gvk)
	if gvkSchema == nil {
		return nil
	}
	patchMeta := strategicpatch.PatchMetaFromOpenAPI{Schema: gvkSchema}
	patch, err := strategicpatch.CreateThreeWayMergePatch(previous, modified, current, patchMeta, true)
	if err != nil {
		klog.Infof("strategic patch unavailable for %s (will use JSON patch instead): %v", resourceDescription, err)
		return nil
	}
	return patch
}

func (c *clientApplier) calculateJSONMerge(previous, modified, current []byte) ([]byte, error) {
	preconditions := []mergepatch.PreconditionFunc{
		mergepatch.RequireKeyUnchanged("apiVersion"),
		mergepatch.RequireKeyUnchanged("kind"),
		mergepatch.RequireMetadataKeyUnchanged("name"),
	}
	return jsonmergepatch.CreateThreeWayJSONMergePatch(previous, modified, current, preconditions...)
}

// isNoOpPatch returns true if the given patch is a no-op that should be ignored.
// TODO: Find a more elegant solution for ignoring noop-like patches.
func isNoOpPatch(patch []byte) bool {
	if patch == nil {
		return true
	}
	p := string(patch)
	return p == noOpPatch || p == rmCreationTimestampPatch
}

// attemptPatch patches resClient with the given patch.
// TODO: Add unit tests for noop logic.
func attemptPatch(ctx context.Context, resClient dynamic.ResourceInterface, name string, patchType types.PatchType, patch []byte) error {
	if isNoOpPatch(patch) {
		return nil
	}

	if err := ctx.Err(); err != nil {
		// We've already encountered an error, so do not attempt update.
		return errors.Wrapf(err, "patch cancelled due to context error")
	}

	start := time.Now()
	_, err := resClient.Patch(ctx, name, patchType, patch, metav1.PatchOptions{})
	duration := time.Since(start).Seconds()
	metrics.APICallDuration.WithLabelValues("update", metrics.StatusLabel(err)).Observe(duration)
	m.RecordAPICallDuration(ctx, "update", m.StatusTagKey(err), start)
	return err
}

// resource retrieves the plural resource name for the GroupVersionKind.
func (c *clientApplier) resource(gvk schema.GroupVersionKind) (string, error) {
	apiResources, err := c.discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return "", errors.Wrapf(err, "could not look up %s using discovery API", gvk)
	}

	for _, r := range apiResources.APIResources {
		if r.Kind == gvk.Kind {
			return r.Name, nil
		}
	}

	return "", errors.Errorf("could not find plural resource name for %s", gvk)
}

func description(u *unstructured.Unstructured) string {
	name := u.GetName()
	namespace := u.GetNamespace()
	gvk := u.GroupVersionKind()
	if namespace == "" {
		return fmt.Sprintf("[%s kind=%s name=%s]", gvk.GroupVersion(), gvk.Kind, name)
	}
	return fmt.Sprintf("[%s kind=%s namespace=%s name=%s]", gvk.GroupVersion(), gvk.Kind, namespace, name)
}

// GetClient returns the underlying applier's client.Client.
func (c *clientApplier) GetClient() client.Client {
	return c.client.Client
}

func equal(dryrunState, currentState *unstructured.Unstructured) bool {
	cleanFields := func(u *unstructured.Unstructured) {
		u.SetGeneration(0)
		u.SetResourceVersion("")
		u.SetManagedFields(nil)
		u.SetCreationTimestamp(metav1.Time{})
		// ignore status field
		unstructured.RemoveNestedField(u.Object, "status")
	}
	obj1 := dryrunState.DeepCopy()
	obj2 := currentState.DeepCopy()
	cleanFields(obj1)
	cleanFields(obj2)
	return equality.Semantic.DeepEqual(obj1.Object, obj2.Object)
}

// ResolveFight implements Applier.
func (c *clientApplier) ResolveFight(now time.Time, obj client.Object) (bool, status.Error) {
	return c.fights.ResolveFight(now, obj)
}
