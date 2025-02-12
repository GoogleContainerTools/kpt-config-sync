// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
)

// RepoLister helps list Repos.
// All objects returned here must be treated as read-only.
type RepoLister interface {
	// List lists all Repos in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*configmanagementv1.Repo, err error)
	// Get retrieves the Repo from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*configmanagementv1.Repo, error)
	RepoListerExpansion
}

// repoLister implements the RepoLister interface.
type repoLister struct {
	listers.ResourceIndexer[*configmanagementv1.Repo]
}

// NewRepoLister returns a new RepoLister.
func NewRepoLister(indexer cache.Indexer) RepoLister {
	return &repoLister{listers.New[*configmanagementv1.Repo](indexer, configmanagementv1.Resource("repo"))}
}
