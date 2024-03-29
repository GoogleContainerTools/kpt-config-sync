// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
)

// ClusterConfigLister helps list ClusterConfigs.
// All objects returned here must be treated as read-only.
type ClusterConfigLister interface {
	// List lists all ClusterConfigs in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.ClusterConfig, err error)
	// Get retrieves the ClusterConfig from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.ClusterConfig, error)
	ClusterConfigListerExpansion
}

// clusterConfigLister implements the ClusterConfigLister interface.
type clusterConfigLister struct {
	indexer cache.Indexer
}

// NewClusterConfigLister returns a new ClusterConfigLister.
func NewClusterConfigLister(indexer cache.Indexer) ClusterConfigLister {
	return &clusterConfigLister{indexer: indexer}
}

// List lists all ClusterConfigs in the indexer.
func (s *clusterConfigLister) List(selector labels.Selector) (ret []*v1.ClusterConfig, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ClusterConfig))
	})
	return ret, err
}

// Get retrieves the ClusterConfig from the index for a given name.
func (s *clusterConfigLister) Get(name string) (*v1.ClusterConfig, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("clusterconfig"), name)
	}
	return obj.(*v1.ClusterConfig), nil
}
