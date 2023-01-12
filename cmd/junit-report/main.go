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
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/junit-report/resetfailure"
)

var (
	rootCmd = &cobra.Command{
		Use:   "junit-report",
		Short: "Postprocess the junit report",
	}
)

func init() {
	rootCmd.AddCommand(resetfailure.Cmd)
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
