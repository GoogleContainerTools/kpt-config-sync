// Copyright 2024 Google LLC
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

package parse

import (
	"context"
	"fmt"

	"github.com/elliotchance/orderedmap/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/cli-utils/pkg/common"
)

type rootSyncParser struct {
	options *RootOptions
}

// ParseSource implements the Parser interface
func (p *rootSyncParser) ParseSource(ctx context.Context, state *sourceState) ([]ast.FileObject, status.MultiError) {
	opts := p.options

	wantFiles := state.files
	if opts.SourceFormat == configsync.SourceFormatHierarchy {
		// We're using hierarchical mode for the root repository, so ignore files
		// outside of the allowed directories.
		wantFiles = filesystem.FilterHierarchyFiles(state.syncPath, wantFiles)
	}

	filePaths := reader.FilePaths{
		RootDir:   state.syncPath,
		PolicyDir: p.options.SyncDir,
		Files:     wantFiles,
	}

	crds, err := opts.DeclaredResources.DeclaredCRDs()
	if err != nil {
		return nil, err
	}
	builder := discovery.ScoperBuilder(opts.DiscoveryClient)

	klog.Infof("Parsing files from source path: %s", state.syncPath.OSPath())
	objs, err := opts.ConfigParser.Parse(filePaths)
	if err != nil {
		return nil, err
	}

	options := validate.Options{
		ClusterName:  opts.ClusterName,
		SyncName:     opts.SyncName,
		PolicyDir:    opts.SyncDir,
		PreviousCRDs: crds,
		BuildScoper:  builder,
		Converter:    opts.Converter,
		// Enable API call so NamespaceSelector can talk to k8s-api-server.
		AllowAPICall:             true,
		DynamicNSSelectorEnabled: opts.DynamicNSSelectorEnabled,
		NSControllerState:        opts.NSControllerState,
		WebhookEnabled:           opts.WebhookEnabled,
		FieldManager:             configsync.FieldManager,
	}
	options = OptionsForScope(options, opts.Scope)

	if opts.SourceFormat == configsync.SourceFormatUnstructured {
		if opts.NamespaceStrategy == configsync.NamespaceStrategyImplicit {
			options.Visitors = append(options.Visitors, p.addImplicitNamespaces)
		}
		objs, err = validate.Unstructured(ctx, opts.Client, objs, options)
	} else {
		objs, err = validate.Hierarchical(objs, options)
	}

	if status.HasBlockingErrors(err) {
		return nil, err
	}

	// Duplicated with namespace.go.
	e := addAnnotationsAndLabels(objs, declared.RootScope, opts.SyncName, opts.Files.sourceContext(), state.commit)
	if e != nil {
		err = status.Append(err, status.InternalErrorf("unable to add annotations and labels: %v", e))
		return nil, err
	}
	return objs, err
}

// addImplicitNamespaces hydrates the given FileObjects by injecting implicit
// namespaces into the list before returning it. Implicit namespaces are those
// that are declared by an object's metadata namespace field but are not present
// in the list. The implicit namespace is only added if it doesn't exist.
func (p *rootSyncParser) addImplicitNamespaces(objs []ast.FileObject) ([]ast.FileObject, status.MultiError) {
	opts := p.options
	var errs status.MultiError
	// namespaces will track the set of Namespaces we expect to exist, and those
	// which actually do.
	namespaces := orderedmap.NewOrderedMap[string, bool]()

	for _, o := range objs {
		if o.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind() {
			namespaces.Set(o.GetName(), true)
		} else if o.GetNamespace() != "" {
			if _, found := namespaces.Get(o.GetNamespace()); !found {
				// If unset, this ensures the key exists and is false.
				// Otherwise it has no impact.
				namespaces.Set(o.GetNamespace(), false)
			}
		}
	}

	for e := namespaces.Front(); e != nil; e = e.Next() {
		ns, isDeclared := e.Key, e.Value
		// Do not treat config-management-system as an implicit namespace for multi-sync support.
		// Otherwise, the namespace will become a managed resource, and will cause conflict among multiple RootSyncs.
		if isDeclared || ns == configsync.ControllerNamespace {
			continue
		}
		existingNs := &corev1.Namespace{}
		err := opts.Client.Get(context.Background(), types.NamespacedName{Name: ns}, existingNs)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = status.Append(errs, fmt.Errorf("unable to check the existence of the implicit namespace %q: %w", ns, err))
			continue
		}

		existingNs.SetGroupVersionKind(kinds.Namespace())
		// If the namespace already exists and not self-managed, do not add it as an implicit namespace.
		// This is to avoid conflicts caused by multiple Root reconcilers managing the same implicit namespace.
		if err == nil && !diff.IsManager(opts.Scope, opts.SyncName, existingNs) {
			continue
		}

		// Add the implicit namespace if it doesn't exist, or if it is managed by itself.
		// If it is a self-managed namespace, still add it to the object list. Otherwise,
		// it will be pruned because it is no longer in the inventory list.
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(kinds.Namespace())
		u.SetName(ns)
		// We do NOT want to delete theses implicit Namespaces when the resources
		// inside them are removed from the repo. We don't know when it is safe to remove
		// the implicit namespaces. An implicit namespace may already exist in the
		// cluster. Deleting it will cause other unmanaged resources in that namespace
		// being deleted.
		//
		// Adding the LifecycleDeleteAnnotation is to prevent the applier from deleting
		// the implicit namespace when the namespaced config is removed from the repo.
		// Note that if the user later declares the
		// Namespace without this annotation, the annotation is removed as expected.
		u.SetAnnotations(map[string]string{common.LifecycleDeleteAnnotation: common.PreventDeletion})
		objs = append(objs, ast.NewFileObject(u, cmpath.RelativeOS("")))
	}

	return objs, errs
}
