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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"text/tabwriter"
	"time"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/client/restconfig"
)

const (
	pendingMsg     = "PENDING"
	syncedMsg      = "SYNCED"
	stalledMsg     = "STALLED"
	reconcilingMsg = "RECONCILING"
)

// ExecutionParams contains all parameters needed to execute the status command
// This struct is completely independent of cobra command structures
type ExecutionParams struct {
	Contexts        []string
	ClientTimeout   time.Duration
	PollingInterval time.Duration
	Namespace       string
	ResourceStatus  bool
	Name            string
}

// ExecuteStatus executes the core status command logic without any cobra dependencies
// This function encapsulates all the business logic for the status command
func ExecuteStatus(ctx context.Context, params ExecutionParams) error {
	fmt.Println("Connecting to clusters...")

	clientMap, err := ClusterClients(ctx, params.Contexts)
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
	if params.PollingInterval > 0 {
		for {
			printStatusWithParams(ctx, writer, clientMap, names, params)
			time.Sleep(params.PollingInterval)
		}
	} else {
		printStatusWithParams(ctx, writer, clientMap, names, params)
	}
	return nil
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

	// Create default execution parameters for temp file generation
	params := ExecutionParams{
		Contexts:        contexts,
		Namespace:       "",
		ResourceStatus:  true,
		Name:            "",
		PollingInterval: 0,
	}

	printStatusWithParams(ctx, writer, clientMap, names, params)
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
func clusterStates(ctx context.Context, clientMap map[string]*ClusterClient, namespace string) (map[string]*ClusterState, []string) {
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

// printStatusWithParams fetches ConfigManagementStatus and/or RepoStatus from each cluster in the given map
// and then prints a formatted status row for each one. If there are any errors reported by either
// object, those are printed in a second table under the status table.
// This function accepts execution parameters instead of relying on global variables
// nolint:errcheck
func printStatusWithParams(ctx context.Context, writer *tabwriter.Writer, clientMap map[string]*ClusterClient, names []string, params ExecutionParams) {
	// First build up a map of all the states to display.
	stateMap, monoRepoClusters := clusterStates(ctx, clientMap, params.Namespace)

	// Log a notice for the detected clusters that are running in the mono-repo mode.
	util.MonoRepoNotice(writer, monoRepoClusters...)

	currentContext, err := restconfig.CurrentContextName()
	if err != nil {
		fmt.Printf("Failed to get current context name with err: %v\n", err)
	}

	// Now we write everything at once. Processing and then printing helps avoid screen strobe.

	if params.PollingInterval > 0 {
		// Clear previous output and flush it to avoid messing up column widths.
		clearTerminal(writer)
		writer.Flush()
	}

	// Print status for each cluster.
	for _, name := range names {
		state := stateMap[name]
		if name == currentContext {
			// Prepend an asterisk for the users' current context
			state.Ref = "*" + name
		}
		state.printRows(writer, params.Name, params.ResourceStatus)
	}

	writer.Flush()
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
