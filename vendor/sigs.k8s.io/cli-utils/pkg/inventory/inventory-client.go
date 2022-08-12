// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Client expresses an interface for interacting with
// objects which store references to objects (inventory objects).
type Client interface {
	// GetClusterObjs returns the set of previously applied objects as ObjMetadata,
	// or an error if one occurred. This set of previously applied object references
	// is stored in the inventory objects living in the cluster.
	GetClusterObjs(inv Info) (object.ObjMetadataSet, error)
	// Merge applies the union of the passed objects with the currently
	// stored objects in the inventory object. Returns the set of
	// objects which are not in the passed objects (objects to be pruned).
	// Otherwise, returns an error if one happened.
	Merge(inv Info, objs object.ObjMetadataSet, dryRun common.DryRunStrategy) (object.ObjMetadataSet, error)
	// Replace replaces the set of objects stored in the inventory
	// object with the passed set of objects, or an error if one occurs.
	Replace(inv Info, objs object.ObjMetadataSet, status []actuation.ObjectStatus, dryRun common.DryRunStrategy) error
	// DeleteInventoryObj deletes the passed inventory object from the APIServer.
	DeleteInventoryObj(inv Info, dryRun common.DryRunStrategy) error
	// ApplyInventoryNamespace applies the Namespace that the inventory object should be in.
	ApplyInventoryNamespace(invNamespace *unstructured.Unstructured, dryRun common.DryRunStrategy) error
	// GetClusterInventoryInfo returns the cluster inventory object.
	GetClusterInventoryInfo(inv Info) (*unstructured.Unstructured, error)
	// GetClusterInventoryObjs looks up the inventory objects from the cluster.
	GetClusterInventoryObjs(inv Info) (object.UnstructuredSet, error)
}

// ClusterClient is a concrete implementation of the
// Client interface.
type ClusterClient struct {
	dc                    dynamic.Interface
	discoveryClient       discovery.CachedDiscoveryInterface
	mapper                meta.RESTMapper
	InventoryFactoryFunc  StorageFactoryFunc
	invToUnstructuredFunc ToUnstructuredFunc
	statusPolicy          StatusPolicy
}

var _ Client = &ClusterClient{}

// NewClient returns a concrete implementation of the
// Client interface or an error.
func NewClient(factory cmdutil.Factory,
	invFunc StorageFactoryFunc,
	invToUnstructuredFunc ToUnstructuredFunc,
	statusPolicy StatusPolicy,
) (*ClusterClient, error) {
	dc, err := factory.DynamicClient()
	if err != nil {
		return nil, err
	}
	mapper, err := factory.ToRESTMapper()
	if err != nil {
		return nil, err
	}
	discoveryClinet, err := factory.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	clusterClient := ClusterClient{
		dc:                    dc,
		discoveryClient:       discoveryClinet,
		mapper:                mapper,
		InventoryFactoryFunc:  invFunc,
		invToUnstructuredFunc: invToUnstructuredFunc,
		statusPolicy:          statusPolicy,
	}
	return &clusterClient, nil
}

// Merge stores the union of the passed objects with the objects currently
// stored in the cluster inventory object. Retrieves and caches the cluster
// inventory object. Returns the set differrence of the cluster inventory
// objects and the currently applied objects. This is the set of objects
// to prune. Creates the initial cluster inventory object storing the passed
// objects if an inventory object does not exist. Returns an error if one
// occurred.
func (cic *ClusterClient) Merge(localInv Info, objs object.ObjMetadataSet, dryRun common.DryRunStrategy) (object.ObjMetadataSet, error) {
	pruneIds := object.ObjMetadataSet{}
	invObj := cic.invToUnstructuredFunc(localInv)
	clusterInv, err := cic.GetClusterInventoryInfo(localInv)
	if err != nil {
		return pruneIds, err
	}
	if clusterInv == nil {
		// Wrap inventory object and store the inventory in it.
		var status []actuation.ObjectStatus
		if cic.statusPolicy == StatusPolicyAll {
			status = getObjStatus(nil, objs)
		}
		inv := cic.InventoryFactoryFunc(invObj)
		if err := inv.Store(objs, status); err != nil {
			return nil, err
		}
		invInfo, err := inv.GetObject()
		if err != nil {
			return nil, err
		}
		klog.V(4).Infof("creating initial inventory object with %d objects", len(objs))
		createdObj, err := cic.createInventoryObj(invInfo, dryRun)
		if err != nil {
			return nil, err
		}
		// Status update requires the latest ResourceVersion
		invInfo.SetResourceVersion(createdObj.GetResourceVersion())
		if err := cic.updateStatus(invInfo, dryRun); err != nil {
			return nil, err
		}
	} else {
		// Update existing cluster inventory with merged union of objects
		clusterObjs, err := cic.GetClusterObjs(localInv)
		if err != nil {
			return pruneIds, err
		}
		pruneIds = clusterObjs.Diff(objs)
		unionObjs := clusterObjs.Union(objs)
		var status []actuation.ObjectStatus
		if cic.statusPolicy == StatusPolicyAll {
			status = getObjStatus(pruneIds, unionObjs)
		}
		klog.V(4).Infof("num objects to prune: %d", len(pruneIds))
		klog.V(4).Infof("num merged objects to store in inventory: %d", len(unionObjs))
		wrappedInv := cic.InventoryFactoryFunc(clusterInv)
		if err = wrappedInv.Store(unionObjs, status); err != nil {
			return pruneIds, err
		}
		clusterInv, err = wrappedInv.GetObject()
		if err != nil {
			return pruneIds, err
		}
		if dryRun.ClientOrServerDryRun() {
			return pruneIds, nil
		}
		if !objs.Equal(clusterObjs) {
			klog.V(4).Infof("update cluster inventory: %s/%s", clusterInv.GetNamespace(), clusterInv.GetName())
			appliedObj, err := cic.applyInventoryObj(clusterInv, dryRun)
			if err != nil {
				return pruneIds, err
			}
			// Status update requires the latest ResourceVersion
			clusterInv.SetResourceVersion(appliedObj.GetResourceVersion())
		}
		if err := cic.updateStatus(clusterInv, dryRun); err != nil {
			return pruneIds, err
		}
	}

	return pruneIds, nil
}

// Replace stores the passed objects in the cluster inventory object, or
// an error if one occurred.
func (cic *ClusterClient) Replace(localInv Info, objs object.ObjMetadataSet, status []actuation.ObjectStatus,
	dryRun common.DryRunStrategy) error {
	// Skip entire function for dry-run.
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infoln("dry-run replace inventory object: not applied")
		return nil
	}
	clusterObjs, err := cic.GetClusterObjs(localInv)
	if err != nil {
		return fmt.Errorf("failed to read inventory objects from cluster: %w", err)
	}
	clusterInv, err := cic.GetClusterInventoryInfo(localInv)
	if err != nil {
		return fmt.Errorf("failed to read inventory from cluster: %w", err)
	}
	clusterInv, err = cic.replaceInventory(clusterInv, objs, status)
	if err != nil {
		return err
	}
	if !objs.Equal(clusterObjs) {
		klog.V(4).Infof("replace cluster inventory: %s/%s", clusterInv.GetNamespace(), clusterInv.GetName())
		klog.V(4).Infof("replace cluster inventory %d objects", len(objs))
		appliedObj, err := cic.applyInventoryObj(clusterInv, dryRun)
		if err != nil {
			return fmt.Errorf("failed to write updated inventory to cluster: %w", err)
		}
		// Status update requires the latest ResourceVersion
		clusterInv.SetResourceVersion(appliedObj.GetResourceVersion())
	}
	if err := cic.updateStatus(clusterInv, dryRun); err != nil {
		return err
	}
	return nil
}

// replaceInventory stores the passed objects into the passed inventory object.
func (cic *ClusterClient) replaceInventory(inv *unstructured.Unstructured, objs object.ObjMetadataSet,
	status []actuation.ObjectStatus) (*unstructured.Unstructured, error) {
	if cic.statusPolicy == StatusPolicyNone {
		status = nil
	}
	wrappedInv := cic.InventoryFactoryFunc(inv)
	if err := wrappedInv.Store(objs, status); err != nil {
		return nil, err
	}
	clusterInv, err := wrappedInv.GetObject()
	if err != nil {
		return nil, err
	}
	return clusterInv, nil
}

// DeleteInventoryObj deletes the inventory object from the cluster.
func (cic *ClusterClient) DeleteInventoryObj(localInv Info, dryRun common.DryRunStrategy) error {
	if localInv == nil {
		return fmt.Errorf("retrieving cluster inventory object with nil local inventory")
	}
	switch localInv.Strategy() {
	case NameStrategy:
		return cic.deleteInventoryObjByName(cic.invToUnstructuredFunc(localInv), dryRun)
	case LabelStrategy:
		return cic.deleteInventoryObjsByLabel(localInv, dryRun)
	default:
		panic(fmt.Errorf("unknown inventory strategy: %s", localInv.Strategy()))
	}
}

func (cic *ClusterClient) deleteInventoryObjsByLabel(inv Info, dryRun common.DryRunStrategy) error {
	clusterInvObjs, err := cic.getClusterInventoryObjsByLabel(inv)
	if err != nil {
		return err
	}
	for _, invObj := range clusterInvObjs {
		if err := cic.deleteInventoryObjByName(invObj, dryRun); err != nil {
			return err
		}
	}
	return nil
}

// GetClusterObjs returns the objects stored in the cluster inventory object, or
// an error if one occurred.
func (cic *ClusterClient) GetClusterObjs(localInv Info) (object.ObjMetadataSet, error) {
	var objs object.ObjMetadataSet
	clusterInv, err := cic.GetClusterInventoryInfo(localInv)
	if err != nil {
		return objs, fmt.Errorf("failed to read inventory from cluster: %w", err)
	}
	// First time; no inventory obj yet.
	if clusterInv == nil {
		return objs, nil
	}
	wrapped := cic.InventoryFactoryFunc(clusterInv)
	return wrapped.Load()
}

// getClusterInventoryObj returns a pointer to the cluster inventory object, or
// an error if one occurred. Returns the cached cluster inventory object if it
// has been previously retrieved. Uses the ResourceBuilder to retrieve the
// inventory object in the cluster, using the namespace, group resource, and
// inventory label. Merges multiple inventory objects into one if it retrieves
// more than one (this should be very rare).
//
// TODO(seans3): Remove the special case code to merge multiple cluster inventory
// objects once we've determined that this case is no longer possible.
func (cic *ClusterClient) GetClusterInventoryInfo(inv Info) (*unstructured.Unstructured, error) {
	clusterInvObjects, err := cic.GetClusterInventoryObjs(inv)
	if err != nil {
		return nil, fmt.Errorf("failed to read inventory objects from cluster: %w", err)
	}

	var clusterInv *unstructured.Unstructured
	if len(clusterInvObjects) == 1 {
		clusterInv = clusterInvObjects[0]
	} else if l := len(clusterInvObjects); l > 1 {
		return nil, fmt.Errorf("found %d inventory objects with inventory id %s", l, inv.ID())
	}
	return clusterInv, nil
}

func (cic *ClusterClient) getClusterInventoryObjsByLabel(inv Info) (object.UnstructuredSet, error) {
	localInv := cic.invToUnstructuredFunc(inv)
	if localInv == nil {
		return nil, fmt.Errorf("retrieving cluster inventory object with nil local inventory")
	}
	localObj := object.UnstructuredToObjMetadata(localInv)
	mapping, err := cic.getMapping(localInv)
	if err != nil {
		return nil, err
	}
	groupResource := mapping.Resource.GroupResource().String()
	namespace := localObj.Namespace
	label, err := retrieveInventoryLabel(localInv)
	if err != nil {
		return nil, err
	}
	labelSelector := fmt.Sprintf("%s=%s", common.InventoryLabel, label)
	klog.V(4).Infof("inventory object fetch by label (group: %q, namespace: %q, selector: %q)", groupResource, namespace, labelSelector)

	uList, err := cic.dc.Resource(mapping.Resource).Namespace(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	var invList []*unstructured.Unstructured
	for i := range uList.Items {
		invList = append(invList, &uList.Items[i])
	}
	return invList, nil
}

func (cic *ClusterClient) getClusterInventoryObjsByName(inv Info) (object.UnstructuredSet, error) {
	localInv := cic.invToUnstructuredFunc(inv)
	if localInv == nil {
		return nil, fmt.Errorf("retrieving cluster inventory object with nil local inventory")
	}

	mapping, err := cic.getMapping(localInv)
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("inventory object fetch by name (namespace: %q, name: %q)", inv.Namespace(), inv.Name())
	clusterInv, err := cic.dc.Resource(mapping.Resource).Namespace(inv.Namespace()).
		Get(context.TODO(), inv.Name(), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if apierrors.IsNotFound(err) {
		return object.UnstructuredSet{}, nil
	}
	return object.UnstructuredSet{clusterInv}, nil
}

func (cic *ClusterClient) GetClusterInventoryObjs(inv Info) (object.UnstructuredSet, error) {
	if inv == nil {
		return nil, fmt.Errorf("inventoryInfo must be specified")
	}

	var clusterInvObjects object.UnstructuredSet
	var err error
	switch inv.Strategy() {
	case NameStrategy:
		clusterInvObjects, err = cic.getClusterInventoryObjsByName(inv)
	case LabelStrategy:
		clusterInvObjects, err = cic.getClusterInventoryObjsByLabel(inv)
	default:
		panic(fmt.Errorf("unknown inventory strategy: %s", inv.Strategy()))
	}
	return clusterInvObjects, err
}

// applyInventoryObj applies the passed inventory object to the APIServer.
func (cic *ClusterClient) applyInventoryObj(obj *unstructured.Unstructured, dryRun common.DryRunStrategy) (*unstructured.Unstructured, error) {
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run apply inventory object: not applied")
		return obj.DeepCopy(), nil
	}
	if obj == nil {
		return nil, fmt.Errorf("attempting apply a nil inventory object")
	}

	mapping, err := cic.getMapping(obj)
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("replacing inventory object: %s/%s", obj.GetNamespace(), obj.GetName())
	return cic.dc.Resource(mapping.Resource).Namespace(obj.GetNamespace()).
		Update(context.TODO(), obj, metav1.UpdateOptions{})
}

// createInventoryObj creates the passed inventory object on the APIServer.
func (cic *ClusterClient) createInventoryObj(obj *unstructured.Unstructured, dryRun common.DryRunStrategy) (*unstructured.Unstructured, error) {
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run create inventory object: not created")
		return obj.DeepCopy(), nil
	}
	if obj == nil {
		return nil, fmt.Errorf("attempting create a nil inventory object")
	}
	// Default inventory name gets random suffix. Fixes problem where legacy
	// inventory templates within same namespace will collide on name.
	err := fixLegacyInventoryName(obj)
	if err != nil {
		return nil, err
	}

	mapping, err := cic.getMapping(obj)
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("creating inventory object: %s/%s", obj.GetNamespace(), obj.GetName())
	return cic.dc.Resource(mapping.Resource).Namespace(obj.GetNamespace()).
		Create(context.TODO(), obj, metav1.CreateOptions{})
}

// deleteInventoryObjByName deletes the passed inventory object from the APIServer, or
// an error if one occurs.
func (cic *ClusterClient) deleteInventoryObjByName(obj *unstructured.Unstructured, dryRun common.DryRunStrategy) error {
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run delete inventory object: not deleted")
		return nil
	}
	if obj == nil {
		return fmt.Errorf("attempting delete a nil inventory object")
	}

	mapping, err := cic.getMapping(obj)
	if err != nil {
		return err
	}

	klog.V(4).Infof("deleting inventory object: %s/%s", obj.GetNamespace(), obj.GetName())
	return cic.dc.Resource(mapping.Resource).Namespace(obj.GetNamespace()).
		Delete(context.TODO(), obj.GetName(), metav1.DeleteOptions{})
}

// ApplyInventoryNamespace creates the passed namespace if it does not already
// exist, or returns an error if one happened. NOTE: No error if already exists.
func (cic *ClusterClient) ApplyInventoryNamespace(obj *unstructured.Unstructured, dryRun common.DryRunStrategy) error {
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run apply inventory namespace (%s): not applied", obj.GetName())
		return nil
	}

	invNamespace := obj.DeepCopy()
	klog.V(4).Infof("applying inventory namespace: %s", obj.GetName())
	object.StripKyamlAnnotations(invNamespace)
	if err := util.CreateApplyAnnotation(invNamespace, unstructured.UnstructuredJSONScheme); err != nil {
		return err
	}

	mapping, err := cic.getMapping(obj)
	if err != nil {
		return err
	}

	_, err = cic.dc.Resource(mapping.Resource).Create(context.TODO(), invNamespace, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// getMapping returns the RESTMapping for the provided resource.
func (cic *ClusterClient) getMapping(obj *unstructured.Unstructured) (*meta.RESTMapping, error) {
	return cic.mapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
}

func (cic *ClusterClient) updateStatus(obj *unstructured.Unstructured, dryRun common.DryRunStrategy) error {
	if cic.statusPolicy != StatusPolicyAll {
		klog.V(4).Infof("inventory status update skipped (StatusPolicy: %s)", cic.statusPolicy)
		return nil
	}
	if dryRun.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run update inventory status: not updated")
		return nil
	}
	status, found, _ := unstructured.NestedMap(obj.UnstructuredContent(), "status")
	if !found {
		return nil
	}
	mapping, err := cic.mapper.RESTMapping(obj.GroupVersionKind().GroupKind())
	if err != nil {
		return nil
	}
	hasStatus, err := cic.hasSubResource(obj.GetAPIVersion(), mapping.Resource.Resource, "status")
	if err != nil {
		return err
	}
	if !hasStatus {
		klog.V(4).Infof("skip updating inventory status")
		return nil
	}

	klog.V(4).Infof("update inventory status")
	resource := cic.dc.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	meta := metav1.TypeMeta{
		Kind:       obj.GetKind(),
		APIVersion: obj.GetAPIVersion(),
	}
	if err = unstructured.SetNestedMap(obj.Object, status, "status"); err != nil {
		return err
	}
	if _, err = resource.UpdateStatus(context.TODO(), obj, metav1.UpdateOptions{TypeMeta: meta}); err != nil {
		return fmt.Errorf("failed to write updated inventory status to cluster: %w", err)
	}
	return nil
}

// hasSubResource checks if a resource has the given subresource using the discovery client.
func (cic *ClusterClient) hasSubResource(groupVersion, resource, subresource string) (bool, error) {
	resources, err := cic.discoveryClient.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return false, err
	}

	for _, r := range resources.APIResources {
		if r.Name == fmt.Sprintf("%s/%s", resource, subresource) {
			return true, nil
		}
	}
	return false, nil
}

// getObjStatus returns the list of object status
// at the beginning of an apply process.
func getObjStatus(pruneIds, unionIds []object.ObjMetadata) []actuation.ObjectStatus {
	status := []actuation.ObjectStatus{}
	for _, obj := range unionIds {
		status = append(status,
			actuation.ObjectStatus{
				ObjectReference: ObjectReferenceFromObjMetadata(obj),
				Strategy:        actuation.ActuationStrategyApply,
				Actuation:       actuation.ActuationPending,
				Reconcile:       actuation.ReconcilePending,
			})
	}
	for _, obj := range pruneIds {
		status = append(status,
			actuation.ObjectStatus{
				ObjectReference: ObjectReferenceFromObjMetadata(obj),
				Strategy:        actuation.ActuationStrategyDelete,
				Actuation:       actuation.ActuationPending,
				Reconcile:       actuation.ReconcilePending,
			})
	}
	return status
}
