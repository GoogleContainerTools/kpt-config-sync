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

// NamespaceConfigsGetter has a method to return a NamespaceConfigInterface.
// A group's client should implement this interface.
type NamespaceConfigsGetter interface {
	NamespaceConfigs() NamespaceConfigInterface
}

// NamespaceConfigInterface has methods to work with NamespaceConfig resources.
type NamespaceConfigInterface interface {
	Create(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.CreateOptions) (*v1.NamespaceConfig, error)
	Update(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.UpdateOptions) (*v1.NamespaceConfig, error)
	UpdateStatus(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.UpdateOptions) (*v1.NamespaceConfig, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.NamespaceConfig, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.NamespaceConfigList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.NamespaceConfig, err error)
	NamespaceConfigExpansion
}

// namespaceConfigs implements NamespaceConfigInterface
type namespaceConfigs struct {
	client rest.Interface
}

// newNamespaceConfigs returns a NamespaceConfigs
func newNamespaceConfigs(c *ConfigmanagementV1Client) *namespaceConfigs {
	return &namespaceConfigs{
		client: c.RESTClient(),
	}
}

// Get takes name of the namespaceConfig, and returns the corresponding namespaceConfig object, and an error if there is any.
func (c *namespaceConfigs) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.NamespaceConfig, err error) {
	result = &v1.NamespaceConfig{}
	err = c.client.Get().
		Resource("namespaceconfigs").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of NamespaceConfigs that match those selectors.
func (c *namespaceConfigs) List(ctx context.Context, opts metav1.ListOptions) (result *v1.NamespaceConfigList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.NamespaceConfigList{}
	err = c.client.Get().
		Resource("namespaceconfigs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested namespaceConfigs.
func (c *namespaceConfigs) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("namespaceconfigs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a namespaceConfig and creates it.  Returns the server's representation of the namespaceConfig, and an error, if there is any.
func (c *namespaceConfigs) Create(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.CreateOptions) (result *v1.NamespaceConfig, err error) {
	result = &v1.NamespaceConfig{}
	err = c.client.Post().
		Resource("namespaceconfigs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(namespaceConfig).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a namespaceConfig and updates it. Returns the server's representation of the namespaceConfig, and an error, if there is any.
func (c *namespaceConfigs) Update(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.UpdateOptions) (result *v1.NamespaceConfig, err error) {
	result = &v1.NamespaceConfig{}
	err = c.client.Put().
		Resource("namespaceconfigs").
		Name(namespaceConfig.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(namespaceConfig).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *namespaceConfigs) UpdateStatus(ctx context.Context, namespaceConfig *v1.NamespaceConfig, opts metav1.UpdateOptions) (result *v1.NamespaceConfig, err error) {
	result = &v1.NamespaceConfig{}
	err = c.client.Put().
		Resource("namespaceconfigs").
		Name(namespaceConfig.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(namespaceConfig).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the namespaceConfig and deletes it. Returns an error if one occurs.
func (c *namespaceConfigs) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("namespaceconfigs").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *namespaceConfigs) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("namespaceconfigs").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched namespaceConfig.
func (c *namespaceConfigs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.NamespaceConfig, err error) {
	result = &v1.NamespaceConfig{}
	err = c.client.Patch(pt).
		Resource("namespaceconfigs").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}