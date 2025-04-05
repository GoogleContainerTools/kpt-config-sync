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
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/cmd/nomos/version"
	"kpt.dev/configsync/cmd/nomos/vet"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
	pkgversion "kpt.dev/configsync/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	// versionTemplate is the template used when "nomos --version" is invoked.
	// The default template outputs "nomos version <VERSION>". This just outputs
	// "<VERSION>" for easier programmatic use.
	versionTemplate = `{{.Version}}
`
)

var (
	rootCmd = &cobra.Command{
		Use:     configmanagement.CLIName,
		Version: pkgversion.VERSION,
		Short: fmt.Sprintf(
			"Set up and manage a Anthos Configuration Management directory (version %v)", pkgversion.VERSION),
	}
)

func init() {
	rootCmd.SetVersionTemplate(versionTemplate)
	rootCmd.AddCommand(initialize.Cmd)
	rootCmd.AddCommand(hydrate.Cmd)
	rootCmd.AddCommand(vet.Cmd)
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(status.Cmd)
	rootCmd.AddCommand(bugreport.Cmd)
	rootCmd.AddCommand(migrate.Cmd)
}

func main() {
	// Use the default flag set, because some libs register flags with init.
	fs := flag.CommandLine

	// Register klog flags
	klog.InitFlags(fs)

	// Work around the controller-runtime init registering a --kubeconfig flag
	// with no default value. Use the same default as kubectl instead.
	// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.16.3/pkg/client/config/config.go#L44
	if f := fs.Lookup(config.KubeconfigFlagName); f != nil {
		// Change the default value & usage.
		// This path should be expected because import inits load first.
		defaultKubeConfigPath, err := restconfig.KubeConfigPath()
		if err != nil {
			klog.Fatal(err)
		}
		f.DefValue = defaultKubeConfigPath
		f.Usage = "Path to the client config file."
	}

	// Cobra uses the pflag lib, instead of the go flag lib.
	// So re-register all go flags as global (aka persistent) pflags.
	rootCmd.PersistentFlags().AddGoFlagSet(fs)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}

	util.MustFprintf(os.Stdout, "Done!\n")
}
