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

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate"
)

type repoSyncParser struct {
	options *Options
}

// ParseSource implements the Parser interface
func (p *repoSyncParser) ParseSource(ctx context.Context, state *sourceState) ([]ast.FileObject, status.MultiError) {
	opts := p.options
	filePaths := reader.FilePaths{
		RootDir:   state.syncPath,
		PolicyDir: opts.SyncDir,
		Files:     state.files,
	}
	crds, err := opts.DeclaredResources.DeclaredCRDs(p.options.Client.Scheme())
	if err != nil {
		return nil, err
	}
	builder := utildiscovery.ScoperBuilder(opts.DiscoveryClient)

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
		Scheme:       opts.Client.Scheme(),
		// Namespaces and NamespaceSelectors should not be declared in a namespace repo.
		// So disable the API call and dynamic mode of NamespaceSelector.
		AllowAPICall:             false,
		DynamicNSSelectorEnabled: false,
		WebhookEnabled:           opts.WebhookEnabled,
		FieldManager:             configsync.FieldManager,
	}
	options = OptionsForScope(options, opts.Scope)

	objs, err = validate.Unstructured(ctx, opts.Client, objs, options)

	if status.HasBlockingErrors(err) {
		return nil, err
	}

	// Duplicated with root.go.
	e := addAnnotationsAndLabels(objs, opts.Scope, opts.SyncName, opts.Files.sourceContext(), state.commit)
	if e != nil {
		err = status.Append(err, status.InternalErrorf("unable to add annotations and labels: %v", e))
		return nil, err
	}
	return objs, err
}
