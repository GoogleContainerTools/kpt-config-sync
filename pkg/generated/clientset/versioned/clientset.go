// Code generated by client-gen. DO NOT EDIT.

package versioned

import (
	"fmt"
	"net/http"

	discovery "k8s.io/client-go/discovery"
	rest "k8s.io/client-go/rest"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
	configmanagementv1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/configmanagement/v1"
	configsyncv1alpha1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/configsync/v1alpha1"
	configsyncv1beta1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/hub/v1"
	kptv1alpha1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/kpt.dev/v1alpha1"
)

type Interface interface {
	Discovery() discovery.DiscoveryInterface
	ConfigmanagementV1() configmanagementv1.ConfigmanagementV1Interface
	ConfigsyncV1alpha1() configsyncv1alpha1.ConfigsyncV1alpha1Interface
	ConfigsyncV1beta1() configsyncv1beta1.ConfigsyncV1beta1Interface
	HubV1() hubv1.HubV1Interface
	KptV1alpha1() kptv1alpha1.KptV1alpha1Interface
}

// Clientset contains the clients for groups.
type Clientset struct {
	*discovery.DiscoveryClient
	configmanagementV1 *configmanagementv1.ConfigmanagementV1Client
	configsyncV1alpha1 *configsyncv1alpha1.ConfigsyncV1alpha1Client
	configsyncV1beta1  *configsyncv1beta1.ConfigsyncV1beta1Client
	hubV1              *hubv1.HubV1Client
	kptV1alpha1        *kptv1alpha1.KptV1alpha1Client
}

// ConfigmanagementV1 retrieves the ConfigmanagementV1Client
func (c *Clientset) ConfigmanagementV1() configmanagementv1.ConfigmanagementV1Interface {
	return c.configmanagementV1
}

// ConfigsyncV1alpha1 retrieves the ConfigsyncV1alpha1Client
func (c *Clientset) ConfigsyncV1alpha1() configsyncv1alpha1.ConfigsyncV1alpha1Interface {
	return c.configsyncV1alpha1
}

// ConfigsyncV1beta1 retrieves the ConfigsyncV1beta1Client
func (c *Clientset) ConfigsyncV1beta1() configsyncv1beta1.ConfigsyncV1beta1Interface {
	return c.configsyncV1beta1
}

// HubV1 retrieves the HubV1Client
func (c *Clientset) HubV1() hubv1.HubV1Interface {
	return c.hubV1
}

// KptV1alpha1 retrieves the KptV1alpha1Client
func (c *Clientset) KptV1alpha1() kptv1alpha1.KptV1alpha1Interface {
	return c.kptV1alpha1
}

// Discovery retrieves the DiscoveryClient
func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	if c == nil {
		return nil
	}
	return c.DiscoveryClient
}

// NewForConfig creates a new Clientset for the given config.
// If config's RateLimiter is not set and QPS and Burst are acceptable,
// NewForConfig will generate a rate-limiter in configShallowCopy.
// NewForConfig is equivalent to NewForConfigAndClient(c, httpClient),
// where httpClient was generated with rest.HTTPClientFor(c).
func NewForConfig(c *rest.Config) (*Clientset, error) {
	configShallowCopy := *c

	if configShallowCopy.UserAgent == "" {
		configShallowCopy.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	// share the transport between all clients
	httpClient, err := rest.HTTPClientFor(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	return NewForConfigAndClient(&configShallowCopy, httpClient)
}

// NewForConfigAndClient creates a new Clientset for the given config and http client.
// Note the http client provided takes precedence over the configured transport values.
// If config's RateLimiter is not set and QPS and Burst are acceptable,
// NewForConfigAndClient will generate a rate-limiter in configShallowCopy.
func NewForConfigAndClient(c *rest.Config, httpClient *http.Client) (*Clientset, error) {
	configShallowCopy := *c
	if configShallowCopy.RateLimiter == nil && configShallowCopy.QPS > 0 {
		if configShallowCopy.Burst <= 0 {
			return nil, fmt.Errorf("burst is required to be greater than 0 when RateLimiter is not set and QPS is set to greater than 0")
		}
		configShallowCopy.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(configShallowCopy.QPS, configShallowCopy.Burst)
	}

	var cs Clientset
	var err error
	cs.configmanagementV1, err = configmanagementv1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.configsyncV1alpha1, err = configsyncv1alpha1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.configsyncV1beta1, err = configsyncv1beta1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.hubV1, err = hubv1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.kptV1alpha1, err = kptv1alpha1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}

	cs.DiscoveryClient, err = discovery.NewDiscoveryClientForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	return &cs, nil
}

// NewForConfigOrDie creates a new Clientset for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *Clientset {
	cs, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return cs
}

// New creates a new Clientset for the given RESTClient.
func New(c rest.Interface) *Clientset {
	var cs Clientset
	cs.configmanagementV1 = configmanagementv1.New(c)
	cs.configsyncV1alpha1 = configsyncv1alpha1.New(c)
	cs.configsyncV1beta1 = configsyncv1beta1.New(c)
	cs.hubV1 = hubv1.New(c)
	cs.kptV1alpha1 = kptv1alpha1.New(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClient(c)
	return &cs
}