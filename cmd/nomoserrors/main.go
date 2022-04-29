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
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomoserrors/examples"
	"kpt.dev/configsync/pkg/status"

	// Ensure it's very unlikely we're missing errors.
	_ "kpt.dev/configsync/pkg/importer/filesystem"
	_ "kpt.dev/configsync/pkg/remediator"
)

var idFlag string

var rootCmd = &cobra.Command{
	Use:   "nomoserrors",
	Short: "List all error codes and example errors",
	Args:  cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		e := examples.Generate()

		if idFlag == "" {
			printErrorCodes()
		}
		idFlag = strings.TrimPrefix(idFlag, "KNV")
		printErrors(idFlag, e)
		printMissingErrors(e)
	},
}

func printMissingErrors(e examples.AllExamples) {
	// Error IDs begin at 1000. Begin at 999 to ensure we catch that KNV1000 has
	// no examples.
	previous := 999
	missing := false
	for _, id := range status.CodeRegistry() {
		idInt, err := strconv.Atoi(id)
		if err != nil {
			fmt.Printf("Non-numeric error ID: %s\n", id)
			continue
		}
		if idInt-previous > 1 && idInt < 2000 {
			// This detects unexpected gaps in error ids. This can happen when we've just
			// added a new package that defines new errors, and none of the packages
			// transitively required by nomoserrors include these errors.
			// The 2000 and up errors are special cases we don't care about for this.
			fmt.Printf("KNV%d must be either explicitly marked obsolete, or its package is not imported\n", previous+1)
			missing = true
		} else if !e[id].Deprecated && len(e[id].Examples) == 0 {
			// The code isn't deprecated and there aren't any examples for it.
			fmt.Printf("Missing example(s) in cmd/nomoserrors/examples/examples.go for code: %s\n", id)
			missing = true
		}
		previous = idInt
	}
	if missing {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&idFlag, "id", "", "if set, only print errors for the passed ID")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func sortedErrors(e examples.AllExamples) []status.Error {
	var allErrs []status.Error
	for _, errs := range e {
		if errs.Deprecated {
			// Ignore explicitly deprecated errors.
			continue
		}
		allErrs = append(allErrs, errs.Examples...)
	}
	sort.Slice(allErrs, func(i, j int) bool {
		return allErrs[i].Error() < allErrs[j].Error()
	})
	return allErrs
}

func printErrorCodes() {
	fmt.Println("=== USED ERROR CODES ===")
	for _, code := range status.CodeRegistry() {
		fmt.Println(code)
	}
	fmt.Println()
}

func printErrors(id string, e examples.AllExamples) {
	printedHeader := false
	for _, err := range sortedErrors(e) {
		if id == "" || err.Code() == id {
			if !printedHeader {
				fmt.Println("=== SAMPLE ERRORS ===")
				fmt.Println()
				printedHeader = true
			}
			fmt.Println(err.Error())
			fmt.Println()
			fmt.Println()
		}
	}
}
