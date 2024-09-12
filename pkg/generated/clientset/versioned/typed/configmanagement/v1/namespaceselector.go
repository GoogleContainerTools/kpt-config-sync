// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	scheme "kpt.dev/configsync/pkg/generated/clientset/versioned/scheme"
)

// NamespaceSelectorsGetter has a method to return a NamespaceSelectorInterface.
// A group's client should implement this interface.
type NamespaceSelectorsGetter interface {
	NamespaceSelectors() NamespaceSelectorInterface
}

// NamespaceSelectorInterface has methods to work with NamespaceSelector resources.
type NamespaceSelectorInterface interface {
	Create(ctx context.Context, namespaceSelector *v1.NamespaceSelector, opts metav1.CreateOptions) (*v1.NamespaceSelector, error)
	Update(ctx context.Context, namespaceSelector *v1.NamespaceSelector, opts metav1.UpdateOptions) (*v1.NamespaceSelector, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.NamespaceSelector, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.NamespaceSelectorList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.NamespaceSelector, err error)
	NamespaceSelectorExpansion
}

// namespaceSelectors implements NamespaceSelectorInterface
type namespaceSelectors struct {
	client rest.Interface
}

// newNamespaceSelectors returns a NamespaceSelectors
func newNamespaceSelectors(c *ConfigmanagementV1Client) *namespaceSelectors {
	return &namespaceSelectors{
		client: c.RESTClient(),
	}
}

// Get takes name of the namespaceSelector, and returns the corresponding namespaceSelector object, and an error if there is any.
func (c *namespaceSelectors) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.NamespaceSelector, err error) {
	result = &v1.NamespaceSelector{}
	err = c.client.Get().
		Resource("namespaceselectors").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of NamespaceSelectors that match those selectors.
func (c *namespaceSelectors) List(ctx context.Context, opts metav1.ListOptions) (result *v1.NamespaceSelectorList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.NamespaceSelectorList{}
	err = c.client.Get().
		Resource("namespaceselectors").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested namespaceSelectors.
func (c *namespaceSelectors) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("namespaceselectors").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a namespaceSelector and creates it.  Returns the server's representation of the namespaceSelector, and an error, if there is any.
func (c *namespaceSelectors) Create(ctx context.Context, namespaceSelector *v1.NamespaceSelector, opts metav1.CreateOptions) (result *v1.NamespaceSelector, err error) {
	result = &v1.NamespaceSelector{}
	err = c.client.Post().
		Resource("namespaceselectors").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(namespaceSelector).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a namespaceSelector and updates it. Returns the server's representation of the namespaceSelector, and an error, if there is any.
func (c *namespaceSelectors) Update(ctx context.Context, namespaceSelector *v1.NamespaceSelector, opts metav1.UpdateOptions) (result *v1.NamespaceSelector, err error) {
	result = &v1.NamespaceSelector{}
	err = c.client.Put().
		Resource("namespaceselectors").
		Name(namespaceSelector.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(namespaceSelector).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the namespaceSelector and deletes it. Returns an error if one occurs.
func (c *namespaceSelectors) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("namespaceselectors").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *namespaceSelectors) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("namespaceselectors").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched namespaceSelector.
func (c *namespaceSelectors) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.NamespaceSelector, err error) {
	result = &v1.NamespaceSelector{}
	err = c.client.Patch(pt).
		Resource("namespaceselectors").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}