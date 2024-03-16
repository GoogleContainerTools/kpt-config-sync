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

package bugreport

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/bugreport"
	"kpt.dev/configsync/pkg/client/restconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", restconfig.DefaultTimeout, "Timeout for connecting to the cluster")
}

// Cmd retrieves readers for all relevant nomos container logs and cluster state commands and writes them to a zip file
var Cmd = &cobra.Command{
	Use:   "bugreport",
	Short: fmt.Sprintf("Generates a zip file of relevant %v debug information.", configmanagement.CLIName),
	Long:  "Generates a zip file in your current directory containing an aggregate of the logs and cluster state for debugging purposes.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		// Send all logs to STDERR.
		if err := cmd.InheritedFlags().Lookup("stderrthreshold").Value.Set("0"); err != nil {
			klog.Errorf("failed to increase logging STDERR threshold: %v", err)
		}

		cfg, err := restconfig.NewRestConfig(flags.ClientTimeout)
		if err != nil {
			return fmt.Errorf("failed to create rest config: %w", err)
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to create kubernetes client set: %w", err)
		}
		c, err := client.New(cfg, client.Options{})
		if err != nil {
			return fmt.Errorf("failed to create kubernetes client: %w", err)
		}

		report, err := bugreport.New(cmd.Context(), c, cs)
		if err != nil {
			return fmt.Errorf("failed to initialize bug reporter: %w", err)
		}

		if err = report.Open(); err != nil {
			return err
		}

		report.WriteRawInZip(report.FetchLogSources(cmd.Context()))
		report.WriteRawInZip(report.FetchResources(cmd.Context()))
		report.WriteRawInZip(report.FetchCMSystemPods(cmd.Context()))
		report.AddNomosStatusToZip(cmd.Context())
		report.AddNomosVersionToZip(cmd.Context())

		report.Close()
		return nil
	},
}
