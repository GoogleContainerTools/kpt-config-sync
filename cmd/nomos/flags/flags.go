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

package flags

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

const (
	// pathFlag is the flag to set the Path of the Nomos directory.
	pathFlag = "path"

	// PathDefault is the default value of the path flag if unset.
	PathDefault = "."

	// contextsFlag is the flag name for the Contexts below.
	contextsFlag = "contexts"

	// clusterFlag is the flag name for the Clusters below.
	clustersFlag = "clusters"

	// SkipAPIServerFlag is the flag name for SkipAPIServer below.
	SkipAPIServerFlag = "no-api-server-check"

	// OutputYAML specifies exporting the output in YAML format.
	OutputYAML = "yaml"

	// OutputJSON specifies exporting the output in JSON format.
	OutputJSON = "json"

	// DefaultHydrationOutput specifies the default location to write the hydrated output.
	DefaultHydrationOutput = "compiled"

	// DefaultClusterClientTimeout specifies the timeout for connecting to each cluster.
	DefaultClusterClientTimeout = 3 * time.Second
)

var (
	// Contexts contains the list of .kubeconfig contexts that are targets of cross-cluster
	// commands.
	Contexts []string

	// Clusters contains the list of Cluster names (specified in clusters/) to perform an action on.
	Clusters []string

	// Path says where the Nomos directory is
	Path string

	// SkipAPIServer directs whether to try to contact the API Server for checks.
	SkipAPIServer bool

	// SourceFormat indicates the format of the Git repository.
	SourceFormat string

	// OutputFormat is the format of output.
	OutputFormat string

	// ClientTimeout is a flag value to specify how long to wait before timeout of client connection.
	ClientTimeout time.Duration
)

// AddContexts adds the --contexts flag.
func AddContexts(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&Contexts, contextsFlag, nil,
		`Accepts a comma-separated list of contexts to use in multi-cluster commands. Defaults to all contexts. Use "" for no contexts.`)
}

// AddClusters adds the --clusters flag.
func AddClusters(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&Clusters, clustersFlag, nil,
		`Accepts a comma-separated list of Cluster names to use in multi-cluster commands. Defaults to all clusters. Use "" for no clusters.`)
}

// AddPath adds the --path flag.
func AddPath(cmd *cobra.Command) {
	cmd.Flags().StringVar(&Path, pathFlag, PathDefault,
		`Root directory to use as an Anthos Configuration Management repository.`)
}

// AddSkipAPIServerCheck adds the --no-api-server-check flag.
func AddSkipAPIServerCheck(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&SkipAPIServer, SkipAPIServerFlag, false,
		"If true, disables talking to the API Server for discovery.")
}

// AllClusters returns true if all clusters should be processed.
func AllClusters() bool {
	return Clusters == nil
}

// AddSourceFormat adds the --source-format flag.
func AddSourceFormat(cmd *cobra.Command) {
	cmd.Flags().StringVar(&SourceFormat, reconcilermanager.SourceFormat, "",
		fmt.Sprintf("Source format of the Git repository. Defaults to %s if not set. Use %s for unstructured repos.",
			filesystem.SourceFormatHierarchy, filesystem.SourceFormatUnstructured))
}

// AddOutputFormat adds the --format flag.
func AddOutputFormat(cmd *cobra.Command) {
	cmd.Flags().StringVar(&OutputFormat, "format", "yaml",
		`Output format. Accepts 'yaml' and 'json'.`)
}
