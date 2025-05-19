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

package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/client/restconfig"
)

const (
	pendingMsg     = "PENDING"
	syncedMsg      = "SYNCED"
	stalledMsg     = "STALLED"
	reconcilingMsg = "RECONCILING"
)

var (
	pollingInterval time.Duration
	namespace       string
	resourceStatus  bool
	syncName        string
	format          string
)

func init() {
	flags.AddContexts(Cmd)
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", restconfig.DefaultTimeout, "Sets the timeout for connecting to each cluster. Defaults to 15 seconds. Example: --timeout=30s")
	Cmd.Flags().DurationVar(&pollingInterval, "poll", 0*time.Second, "Continuously polls for status updates at the specified interval. If not provided, the command runs only once. Example: --poll=30s for polling every 30 seconds")
	Cmd.Flags().StringVar(&namespace, "namespace", "", "Filters the status output by the specified RootSync or RepoSync namespace. If not provided, displays status for all RootSync and RepoSync objects.")
	Cmd.Flags().BoolVar(&resourceStatus, "resources", true, "Displays detailed status for individual resources managed by RootSync or RepoSync objects. Defaults to true.")
	Cmd.Flags().StringVar(&syncName, "name", "", "Filters the status output by the specified RootSync or RepoSync name.")
	Cmd.Flags().StringVar(&format, "format", "", fmt.Sprintf("Output format. Accepts '%s'.", flags.OutputJSON))
}

// SaveToTempFile writes the `nomos status` output into a temporary file, and
// opens the file for reading. It returns the file descriptor with read_only permission.
// Using the temp file instead of os.Pipe is to avoid the hanging issue
// caused by the os.Pipe buffer limit: 64k.
// This function is only used in `nomos bugreport` for the `nomos status` output.
func SaveToTempFile(ctx context.Context, contexts []string) (*os.File, error) {
	tmpFile, err := os.CreateTemp(os.TempDir(), "nomos-status-")
	if err != nil {
		return nil, fmt.Errorf("failed to create a temporary file: %w", err)
	}
	writer := util.NewWriter(tmpFile)

	clientMap, err := ClusterClients(ctx, contexts)
	if err != nil {
		return tmpFile, err
	}
	names := clusterNames(clientMap)

	if err := printStatus(ctx, writer, clientMap, names, format); err != nil {
		return tmpFile, fmt.Errorf("failed to print status output: %w", err)
	}
	err = tmpFile.Close()
	if err != nil {
		return tmpFile, fmt.Errorf("failed to close status file writer with error: %w", err)
	}

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		return tmpFile, fmt.Errorf("failed to open the file for reading: %w", err)
	}

	return f, nil
}

// Cmd runs a loop that fetches ACM objects from all available clusters and prints a summary of the
// status of Config Management for each cluster.
var Cmd = &cobra.Command{
	Use: "status",
	// TODO: make Configuration Management a constant (for product renaming)
	Short: `Prints the status of all clusters with Configuration Management installed.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if format != "" && format != flags.OutputJSON {
			return fmt.Errorf("--format only accepts %q", flags.OutputJSON)
		}
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		fmt.Println("Connecting to clusters...")

		clientMap, err := ClusterClients(cmd.Context(), flags.Contexts)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to create client configs: %w", err)
			}

			klog.Fatalf("Failed to get clients: %v", err)
		}
		if len(clientMap) == 0 {
			return errors.New("no clusters found")
		}

		// Use a sorted order of names to avoid shuffling in the output.
		names := clusterNames(clientMap)

		writer := util.NewWriter(os.Stdout)
		if pollingInterval > 0 {
			for {
				if err := printStatus(cmd.Context(), writer, clientMap, names, format); err != nil {
					return fmt.Errorf("failed to print status: %w", err)
				}
				time.Sleep(pollingInterval)
			}
		} else {
			if err := printStatus(cmd.Context(), writer, clientMap, names, format); err != nil {
				return fmt.Errorf("failed to print status: %w", err)
			}
		}
		return nil
	},
}

// clusterNames returns a sorted list of names from the given clientMap.
func clusterNames(clientMap map[string]*ClusterClient) []string {
	var names []string
	for name := range clientMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// clusterStates returns a map of clusterStates calculated from the given map of
// clients, and a list of clusters running in the mono-repo mode.
func clusterStates(ctx context.Context, clientMap map[string]*ClusterClient) (map[string]*ClusterState, []string) {
	stateMap := make(map[string]*ClusterState)
	var monoRepoClusters []string
	for name, client := range clientMap {
		if client == nil {
			stateMap[name] = unavailableCluster(name)
		} else {
			cs := client.clusterStatus(ctx, name, namespace)
			stateMap[name] = cs
			if cs.isMulti != nil && !*cs.isMulti {
				monoRepoClusters = append(monoRepoClusters, name)
			}
		}
	}
	return stateMap, monoRepoClusters
}

// outputClusterStates converts the map of clusterStates to a map of ClusterStateOutput objects.
func outputClusterStates(clusterStates map[string]*ClusterState, names []string) map[string]*ClusterStateOutput {
	result := map[string]*ClusterStateOutput{}

	for _, name := range names {
		state := clusterStates[name]

		// Convert to JSON output object
		result[name] = state.toClusterStateOutput()
	}

	return result
}

// printStatusJSON prints the status of each cluster in JSON format.
// It's used when the `nomos status --format=json` command is invoked.
func printStatusJSON(writer *tabwriter.Writer, jsonClusterStates map[string]*ClusterStateOutput) error {
	json, err := json.MarshalIndent(jsonClusterStates, "", "  ")
	if err != nil {
		return fmt.Errorf("Failed to convert state to: %w", err)
	}

	json = append(json, '\n')

	if _, err := writer.Write(json); err != nil {
		return fmt.Errorf("Failed to print JSON status: %w", err)
	}
	return nil
}

// printStatus fetches ConfigManagementStatus and/or RepoStatus from each cluster in the given map
// and then prints a formatted status row for each one. If there are any errors reported by either
// object, those are printed in a second table under the status table.
func printStatus(ctx context.Context, writer *tabwriter.Writer, clientMap map[string]*ClusterClient, names []string, format string) error {
	// First build up a map of all the states to display.
	stateMap, monoRepoClusters := clusterStates(ctx, clientMap)

	// Log a notice for the detected clusters that are running in the mono-repo mode.
	util.MonoRepoNotice(writer, monoRepoClusters...)

	currentContext, err := restconfig.CurrentContextName()
	if err != nil {
		return fmt.Errorf("Failed to get current context name: %w", err)
	}

	// Now we write everything at once. Processing and then printing helps avoid screen strobe.

	if pollingInterval > 0 {
		// Clear previous output and flush it to avoid messing up column widths.
		clearTerminal(writer)

		if err := writer.Flush(); err != nil {
			return fmt.Errorf("Failed to flush writer: %w", err)
		}
	}

	outputClusterStates := outputClusterStates(stateMap, names)

	if format == "json" {
		if err := printStatusJSON(writer, outputClusterStates); err != nil {
			return fmt.Errorf("Failed to print status in JSON format: %w", err)
		}
	} else {
		for _, name := range names {
			state, ok := outputClusterStates[name]

			if !ok {
				fmt.Printf("Cluster %s not found in cluster states\n", name)
				continue
			}

			if name == currentContext {
				// Prepend an asterisk for the users' current context
				state.Name = "*" + state.Name
			}
			state.printRows(writer)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("Failed to flush writer: %v\n", err)
	}

	return nil
}

// clearTerminal executes an OS-specific command to clear all output on the terminal.
func clearTerminal(out io.Writer) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "cls")
	default:
		cmd = exec.Command("clear")
	}

	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		klog.Warningf("Failed to execute command: %v", err)
	}
}
