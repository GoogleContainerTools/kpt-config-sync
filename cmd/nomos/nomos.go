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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/bugreport"
	"kpt.dev/configsync/cmd/nomos/hydrate"
	"kpt.dev/configsync/cmd/nomos/initialize"
	"kpt.dev/configsync/cmd/nomos/migrate"
	"kpt.dev/configsync/cmd/nomos/status"
	"kpt.dev/configsync/cmd/nomos/version"
	"kpt.dev/configsync/cmd/nomos/vet"
	"kpt.dev/configsync/pkg/api/configmanagement"
	pkgversion "kpt.dev/configsync/pkg/version"
)

var (
	rootCmd = &cobra.Command{
		Use: configmanagement.CLIName,
		Short: fmt.Sprintf(
			"Set up and manage a Anthos Configuration Management directory (version %v)", pkgversion.VERSION),
	}
)

func init() {
	rootCmd.AddCommand(initialize.Cmd)
	rootCmd.AddCommand(hydrate.Cmd)
	rootCmd.AddCommand(vet.Cmd)
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(status.Cmd)
	rootCmd.AddCommand(bugreport.Cmd)
	rootCmd.AddCommand(migrate.Cmd)
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
