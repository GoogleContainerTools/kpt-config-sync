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

package fake

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusWriter is a fake implementation of client.StatusWriter.
type statusWriter struct {
	Client *Client
}

var _ client.StatusWriter = &statusWriter{}

// Client is a fake implementation of client.Client.
type Client struct {
	scheme  *runtime.Scheme
	Objects map[core.ID]client.Object
}

var _ client.Client = &Client{}

// NewClient instantiates a new fake.Client pre-populated with the specified
// objects.
//
// Calls t.Fatal if unable to properly instantiate Client.
func NewClient(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *Client {
	t.Helper()

	result := Client{
		scheme:  scheme,
		Objects: make(map[core.ID]client.Object),
	}

	err := v1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	err = configsyncv1beta1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	err = corev1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	for _, o := range objs {
		err = result.Create(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
	}

	return &result
}

func toGR(gk schema.GroupKind) schema.GroupResource {
	return schema.GroupResource{
		Group:    gk.Group,
		Resource: gk.Kind,
	}
}

// Get implements client.Client.
func (c *Client) Get(_ context.Context, key client.ObjectKey, obj client.Object) error {
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	if obj.GetObjectKind().GroupVersionKind().Empty() {
		// Since many times we call with just an empty struct with no type metadata.
		gvks, _, err := c.Scheme().ObjectKinds(obj)
		if err != nil {
			return err
		}
		switch len(gvks) {
		case 0:
			return errors.Errorf("unregistered Type; register it in fake.Client.Schema: %T", obj)
		case 1:
			obj.GetObjectKind().SetGroupVersionKind(gvks[0])
		default:
			return errors.Errorf("fake.Client does not support multiple Versions for the same GroupKind: %v", obj)
		}
	}

	id := core.IDOf(obj)
	o, ok := c.Objects[id]
	if !ok {
		return newNotFound(id)
	}

	// The actual Kubernetes implementation is much more complex.
	// This approximates the behavior, but will fail (for example) if obj lies
	// about its GroupVersionKind.
	jsn, err := json.Marshal(o)
	if err != nil {
		return errors.Wrapf(err, "unable to Marshal %v", obj)
	}
	err = json.Unmarshal(jsn, obj)
	if err != nil {
		return errors.Wrapf(err, "unable to Unmarshal: %s", string(jsn))
	}
	return nil
}

func validateListOptions(opts client.ListOptions) error {
	if opts.Continue != "" {
		return errors.Errorf("fake.Client.List does not yet support the Continue option, but got: %+v", opts)
	}
	if opts.Limit != 0 {
		return errors.Errorf("fake.Client.List does not yet support the Limit option, but got: %+v", opts)
	}

	return nil
}

// List implements client.Client.
//
// Does not paginate results.
func (c *Client) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	options := client.ListOptions{}
	options.ApplyOptions(opts)
	err := validateListOptions(options)
	if err != nil {
		return err
	}

	_, isList := list.(meta.List)
	if !isList {
		return errors.Errorf("called fake.Client.List on non-List type %T", list)
	}

	if ul, isUnstructured := list.(*unstructured.UnstructuredList); isUnstructured {
		return c.listUnstructured(ul, options)
	}

	switch l := list.(type) {
	case *apiextensionsv1.CustomResourceDefinitionList:
		return c.listV1CRDs(l, options)
	case *v1.SyncList:
		return c.listSyncs(l, options)
	case *configsyncv1beta1.RootSyncList:
		return c.listRootSyncs(l, options)
	case *configsyncv1beta1.RepoSyncList:
		return c.listRepoSyncs(l, options)
	case *corev1.SecretList:
		return c.listSecrets(l, options)
	case *corev1.NodeList:
		return c.listNodes(l, options)
	}
	return errors.Errorf("fake.Client does not support List(%T)", list)
}

func (c *Client) fromUnstructured(obj client.Object) (client.Object, error) {
	// If possible, we want to deal with the non-Unstructured form of objects.
	// Unstructureds are prone to declare a bunch of empty maps we don't care
	// about, and can't easily tell cmp.Diff to ignore.

	u, isUnstructured := obj.(*unstructured.Unstructured)
	if !isUnstructured {
		// Already not unstructured.
		return obj, nil
	}

	result, err := c.Scheme().New(u.GroupVersionKind())
	if err != nil {
		// The type isn't registered.
		return obj, nil
	}

	jsn, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(jsn, result)
	return result.(client.Object), err
}

func validateCreateOptions(opts *client.CreateOptions) error {
	if len(opts.DryRun) > 0 {
		if len(opts.DryRun) > 1 || opts.DryRun[0] != metav1.DryRunAll {
			return errors.Errorf("invalid dry run option: %+v", opts.DryRun)
		}
	}
	if opts.FieldManager != "" && opts.FieldManager != configsync.FieldManager {
		return errors.Errorf("invalid field manager option: %v", opts.FieldManager)
	}
	return nil
}

// Create implements client.Client.
func (c *Client) Create(_ context.Context, obj client.Object, opts ...client.CreateOption) error {
	createOpts := &client.CreateOptions{}
	createOpts.ApplyOptions(opts)
	err := validateCreateOptions(createOpts)
	if err != nil {
		return err
	}

	obj, err = c.fromUnstructured(obj.DeepCopyObject().(client.Object))
	if err != nil {
		return err
	}

	id := core.IDOf(obj)
	_, found := c.Objects[core.IDOf(obj)]
	if found {
		return newAlreadyExists(id)
	}

	c.Objects[id] = obj
	return nil
}

func validateDeleteOptions(opts []client.DeleteOption) error {
	var unsupported []client.DeleteOption
	for _, opt := range opts {
		switch opt {
		case client.PropagationPolicy(metav1.DeletePropagationBackground):
		default:
			unsupported = append(unsupported, opt)
		}
	}
	if len(unsupported) > 0 {
		jsn, _ := json.MarshalIndent(opts, "", "  ")
		return errors.Errorf("fake.Client.Delete does not yet support opts, but got: %v", string(jsn))
	}

	return nil
}

// Delete implements client.Client.
func (c *Client) Delete(_ context.Context, obj client.Object, opts ...client.DeleteOption) error {
	err := validateDeleteOptions(opts)
	if err != nil {
		return err
	}

	id := core.IDOf(obj)

	_, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	// Delete objects whose ownerRef is the current obj.
	for _, o := range c.Objects {
		for _, ownerRef := range o.GetOwnerReferences() {
			if ownerRef.Name == id.Name && ownerRef.Kind == id.Kind {
				delete(c.Objects, core.IDOf(o))
			}
		}
	}
	delete(c.Objects, id)

	return nil
}

func toUnstructured(obj client.Object) (unstructured.Unstructured, error) {
	innerObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	return unstructured.Unstructured{Object: innerObj}, nil
}

func getStatusFromObject(obj client.Object) (map[string]interface{}, bool, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, false, err
	}
	return unstructured.NestedMap(u.Object, "status")
}

func (c *Client) updateObjectStatus(obj client.Object, status map[string]interface{}) (client.Object, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}

	if err = unstructured.SetNestedMap(u.Object, status, "status"); err != nil {
		return obj, err
	}

	updated := &unstructured.Unstructured{Object: u.Object}
	updated.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
	return c.fromUnstructured(updated)
}

func validateUpdateOptions(opts *client.UpdateOptions) error {
	if len(opts.DryRun) > 0 {
		if len(opts.DryRun) > 1 || opts.DryRun[0] != metav1.DryRunAll {
			return errors.Errorf("invalid dry run option: %+v", opts.DryRun)
		}
	}
	if opts.FieldManager != "" && opts.FieldManager != configsync.FieldManager {
		return errors.Errorf("invalid field manager option: %v", opts.FieldManager)
	}
	return nil
}

// Update implements client.Client. It does not update the status field.
func (c *Client) Update(_ context.Context, obj client.Object, opts ...client.UpdateOption) error {
	updateOpts := &client.UpdateOptions{}
	updateOpts.ApplyOptions(opts)
	err := validateUpdateOptions(updateOpts)
	if err != nil {
		return err
	}

	obj, err = c.fromUnstructured(obj.DeepCopyObject().(client.Object))
	if err != nil {
		return err
	}

	id := core.IDOf(obj)

	_, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	oldStatus, hasStatus, err := getStatusFromObject(c.Objects[id])
	if err != nil {
		return err
	}
	if len(updateOpts.DryRun) > 0 {
		// don't merge or store the result
		return nil
	}
	if hasStatus {
		u, err := c.updateObjectStatus(obj, oldStatus)
		if err != nil {
			return err
		}

		c.Objects[id] = u
	} else {
		c.Objects[id] = obj
	}
	return nil
}

// Patch implements client.Client.
func (c *Client) Patch(ctx context.Context, obj client.Object, _ client.Patch, opts ...client.PatchOption) error {
	// Currently re-using the Update implementation for Patch since it fits the use-case where this is used for unit tests.
	// Please use this with caution for your use-case.
	var updateOpts []client.UpdateOption
	for _, opt := range opts {
		if uOpt, ok := opt.(client.UpdateOption); ok {
			updateOpts = append(updateOpts, uOpt)
		} else if opt == client.ForceOwnership {
			// Ignore, for now.
			// TODO: Simulate FieldManagement and Force updates.
		} else {
			return errors.Errorf("invalid patch option: %+v", opt)
		}
	}
	return c.Update(ctx, obj, updateOpts...)
}

// DeleteAllOf implements client.Client.
func (c *Client) DeleteAllOf(_ context.Context, _ client.Object, _ ...client.DeleteAllOfOption) error {
	return errors.New("fake.Client does not support DeleteAllOf()")
}

// Update implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Update(_ context.Context, obj client.Object, opts ...client.UpdateOption) error {
	updateOpts := &client.UpdateOptions{}
	updateOpts.ApplyOptions(opts)
	err := validateUpdateOptions(updateOpts)
	if err != nil {
		return err
	}

	obj, err = s.Client.fromUnstructured(obj.DeepCopyObject().(client.Object))
	if err != nil {
		return err
	}

	id := core.IDOf(obj)

	_, found := s.Client.Objects[id]
	if !found {
		return newNotFound(id)
	}

	newStatus, hasStatus, err := getStatusFromObject(obj)
	if err != nil {
		return err
	}
	if hasStatus {
		if len(updateOpts.DryRun) > 0 {
			// don't merge or store the result
			return nil
		}

		u, err := s.Client.updateObjectStatus(s.Client.Objects[id], newStatus)
		if err != nil {
			return err
		}

		s.Client.Objects[id] = u
	} else {
		return errors.Errorf("the object %q/%q does not have a status field", obj.GetObjectKind().GroupVersionKind(), obj.GetName())
	}
	return nil
}

// Patch implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Patch(ctx context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return s.Update(ctx, obj)
}

// Status implements client.Client.
func (c *Client) Status() client.StatusWriter {
	return &statusWriter{
		Client: c,
	}
}

// Check reports an error to `t` if the passed objects in wants do not match the
// expected set of objects in the fake.Client, and only the passed updates to
// Status fields were recorded.
func (c *Client) Check(t *testing.T, wants ...client.Object) {
	t.Helper()

	wantMap := make(map[core.ID]client.Object)

	for _, obj := range wants {
		obj, err := c.fromUnstructured(obj)
		if err != nil {
			// This is a test precondition, and if it fails the following error
			// messages will be garbage.
			t.Fatal(err)
		}

		cobj, ok := obj.(client.Object)
		if !ok {
			t.Errorf("obj is not a Kubernetes object %v", obj)
		}
		wantMap[core.IDOf(cobj)] = cobj
	}

	checked := make(map[core.ID]bool)
	for id, want := range wantMap {
		checked[id] = true
		actual, found := c.Objects[id]
		if !found {
			t.Errorf("fake.Client missing %s", id.String())
			continue
		}

		_, wantUnstructured := want.(*unstructured.Unstructured)
		_, actualUnstructured := actual.(*unstructured.Unstructured)
		if wantUnstructured != actualUnstructured {
			// If you see this error, you should register the type so the code can
			// compare them properly.
			t.Errorf("got want.(type)=%T and actual.(type)=%T for two objects of type %s, want equal",
				want, actual, want.GetObjectKind().GroupVersionKind().String())
			continue
		}

		if diff := cmp.Diff(want, actual, cmpopts.EquateEmpty()); diff != "" {
			// If you're seeing errors originating from how unstructured conversions work,
			// e.g. the diffs are a bunch of nil maps, then register the type in the
			// client's scheme.
			t.Errorf("diff to fake.Client.Objects[%s]:\n%s", id.String(), diff)
		}
	}
	for id := range c.Objects {
		if !checked[id] {
			t.Errorf("fake.Client unexpectedly contains %s", id.String())
		}
	}
}

func newNotFound(id core.ID) error {
	return apierrors.NewNotFound(toGR(id.GroupKind), id.ObjectKey.String())
}

func newAlreadyExists(id core.ID) error {
	return apierrors.NewAlreadyExists(toGR(id.GroupKind), id.ObjectKey.String())
}

func (c *Client) list(gk schema.GroupKind) []client.Object {
	var result []client.Object
	for _, o := range c.Objects {
		if o.GetObjectKind().GroupVersionKind().GroupKind() != gk {
			continue
		}
		result = append(result, o)
	}
	return result
}

func (c *Client) listV1CRDs(list *apiextensionsv1.CustomResourceDefinitionList, options client.ListOptions) error {
	if options.FieldSelector != nil {
		return errors.Errorf("fake.Client.List for CustomResourceDefinitionList does not yet support the FieldSelector option, but got: %+v", options)
	}
	objs := c.list(kinds.CustomResourceDefinition())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		switch o := obj.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			list.Items = append(list.Items, *o)
		case *v1beta1.CustomResourceDefinition:
			crd, err := clusterconfig.V1Beta1ToV1CRD(o)
			if err != nil {
				return err
			}
			list.Items = append(list.Items, *crd)
		case *unstructured.Unstructured:
			crd, err := clusterconfig.AsV1CRD(o)
			if err != nil {
				return err
			}
			list.Items = append(list.Items, *crd)
		default:
			return errors.Errorf("non-CRD stored as CRD: %+v", obj)
		}
	}

	return nil
}

func (c *Client) listNodes(list *corev1.NodeList, options client.ListOptions) error {
	if options.FieldSelector != nil {
		return errors.Errorf("fake.Client.List for NodeList does not yet support the FieldSelector option, but got: %+v", options)
	}
	objs := c.list(corev1.SchemeGroupVersion.WithKind("Node").GroupKind())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		node, ok := obj.(*corev1.Node)
		if !ok {
			return errors.Errorf("non-Node stored as Node: %v", obj)
		}
		list.Items = append(list.Items, *node)
	}

	return nil
}

func (c *Client) listSyncs(list *v1.SyncList, options client.ListOptions) error {
	if options.FieldSelector != nil {
		return errors.Errorf("fake.Client.List for SyncList does not yet support the FieldSelector option, but got: %+v", options)
	}
	objs := c.list(kinds.Sync().GroupKind())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		sync, ok := obj.(*v1.Sync)
		if !ok {
			return errors.Errorf("non-Sync stored as Sync: %v", obj)
		}
		list.Items = append(list.Items, *sync)
	}

	return nil
}

func (c *Client) listRootSyncs(list *configsyncv1beta1.RootSyncList, options client.ListOptions) error {
	objs := c.list(kinds.RootSyncV1Beta1().GroupKind())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		if options.FieldSelector != nil {
			fieldSelector := strings.Split(options.FieldSelector.String(), "=")
			field := fieldSelector[0]
			if len(fieldSelector) != 2 {
				return errors.Errorf("fake.Client.List for RootSyncList only supports OneTermEqualSelector FieldSelector option, but got: %+v", options)
			}
			uo, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				return err
			}
			var fs []string
			for _, s := range strings.Split(field, ".") {
				if len(s) > 0 {
					fs = append(fs, s)
				}
			}
			val, found, err := unstructured.NestedString(uo, fs...)
			if err != nil || !found {
				continue
			}
			actualFields := fields.Set{field: val}
			if !options.FieldSelector.Matches(actualFields) {
				continue
			}
		}
		rs, ok := obj.(*configsyncv1beta1.RootSync)
		if !ok {
			return errors.Errorf("non-RootSync stored as RootSync: %v", obj)
		}
		list.Items = append(list.Items, *rs)
	}
	return nil
}

func (c *Client) listRepoSyncs(list *configsyncv1beta1.RepoSyncList, options client.ListOptions) error {
	objs := c.list(kinds.RepoSyncV1Beta1().GroupKind())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		if options.FieldSelector != nil {
			fieldSelector := strings.Split(options.FieldSelector.String(), "=")
			field := fieldSelector[0]
			if len(fieldSelector) != 2 {
				return errors.Errorf("fake.Client.List for RepoSyncList only supports OneTermEqualSelector FieldSelector option, but got: %+v", options)
			}
			uo, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				return err
			}
			var fs []string
			for _, s := range strings.Split(field, ".") {
				if len(s) > 0 {
					fs = append(fs, s)
				}
			}
			val, found, err := unstructured.NestedString(uo, fs...)
			if err != nil || !found {
				continue
			}
			actualFields := fields.Set{field: val}
			if !options.FieldSelector.Matches(actualFields) {
				continue
			}
		}
		rs, ok := obj.(*configsyncv1beta1.RepoSync)
		if !ok {
			return errors.Errorf("non-RepoSync stored as RepoSync: %v", obj)
		}
		list.Items = append(list.Items, *rs)
	}
	return nil
}

func (c *Client) listSecrets(list *corev1.SecretList, options client.ListOptions) error {
	if options.FieldSelector != nil {
		return errors.Errorf("fake.Client.List for SecretList does not yet support the FieldSelector option, but got: %+v", options)
	}
	objs := c.list(kinds.Secret().GroupKind())
	for _, obj := range objs {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		s, ok := obj.(*corev1.Secret)
		if !ok {
			return errors.Errorf("non-Secret stored as Secret: %v", obj)
		}
		list.Items = append(list.Items, *s)
	}
	return nil
}

func (c *Client) listUnstructured(list *unstructured.UnstructuredList, options client.ListOptions) error {
	if options.FieldSelector != nil {
		return errors.Errorf("fake.Client.List for UnstructuredList does not yet support the FieldSelector option, but got: %+v", options)
	}
	gvk := list.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		return errors.Errorf("fake.Client.List(UnstructuredList) requires GVK")
	}
	if !strings.HasSuffix(gvk.Kind, "List") {
		return errors.Errorf("fake.Client.List(UnstructuredList) called with non-List GVK %q", gvk.String())
	}
	gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")

	for _, obj := range c.list(gvk.GroupKind()) {
		if options.Namespace != "" && obj.GetNamespace() != options.Namespace {
			continue
		}
		if options.LabelSelector != nil {
			l := labels.Set(obj.GetLabels())
			if !options.LabelSelector.Matches(l) {
				continue
			}
		}
		uo, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return err
		}
		list.Items = append(list.Items, unstructured.Unstructured{Object: uo})
	}
	return nil
}

// Applier returns a fake.Applier wrapping this fake.Client. Callers using the
// resulting Applier will read from/write to the original fake.Client.
func (c *Client) Applier() reconcile.Applier {
	return &applier{Client: c}
}

// Scheme implements client.Client.
func (c *Client) Scheme() *runtime.Scheme {
	return c.scheme
}

// RESTMapper implements client.Client.
func (c *Client) RESTMapper() meta.RESTMapper {
	panic("fake.Client does not support RESTMapper()")
}
