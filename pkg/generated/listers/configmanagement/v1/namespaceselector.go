// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
)

// NamespaceSelectorLister helps list NamespaceSelectors.
// All objects returned here must be treated as read-only.
type NamespaceSelectorLister interface {
	// List lists all NamespaceSelectors in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*configmanagementv1.NamespaceSelector, err error)
	// Get retrieves the NamespaceSelector from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*configmanagementv1.NamespaceSelector, error)
	NamespaceSelectorListerExpansion
}

// namespaceSelectorLister implements the NamespaceSelectorLister interface.
type namespaceSelectorLister struct {
	listers.ResourceIndexer[*configmanagementv1.NamespaceSelector]
}

// NewNamespaceSelectorLister returns a new NamespaceSelectorLister.
func NewNamespaceSelectorLister(indexer cache.Indexer) NamespaceSelectorLister {
	return &namespaceSelectorLister{listers.New[*configmanagementv1.NamespaceSelector](indexer, configmanagementv1.Resource("namespaceselector"))}
}
