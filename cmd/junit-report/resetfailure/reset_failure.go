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

package resetfailure

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"

	"github.com/jstemmer/go-junit-report/v2/junit"
	"github.com/spf13/cobra"
)

var reportFile string

func init() {
	Cmd.Flags().StringVar(&reportFile, "path", "",
		"The file path to the junit report")
}

// Cmd is the Cobra object representing the junit-report reset-failure command
var Cmd = &cobra.Command{
	Use:     "reset-failure",
	Short:   "Add an empty Failure entry to the junit report",
	Long:    `Add an empty Failure entry to the junit report`,
	Example: `junit-report reset-failure --path /logs/artifacts/junit_report.xml`,
	Args:    cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		return ResetFailure(reportFile)
	},
}

// ResetFailure adds a Failure entry to the report.
func ResetFailure(path string) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	testSuites := &junit.Testsuites{}
	if err = xml.Unmarshal(bytes, testSuites); err != nil {
		return err
	}

	failureTestSuite := junit.Testsuite{
		Name: "kpt.dev/configsync/e2e/testcases",
		ID:   len(testSuites.Suites),
		Time: "0",
		Testcases: []junit.Testcase{
			{
				Name:      "Failure",
				Classname: "kpt.dev/configsync/e2e/testcases",
				Time:      "0",
			},
		},
	}

	testSuites.AddSuite(failureTestSuite)
	return updateReport(testSuites, path)
}

func updateReport(t *junit.Testsuites, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return writeXML(t, f)
}

// copied from https://github.com/jstemmer/go-junit-report/blob/075629ad5f2934f016fa8fe79deb821f98bd8b44/junit/junit.go#L41
// TODO: remove the duplicate when a new go-junit-report release is available.
func writeXML(t *junit.Testsuites, w io.Writer) error {
	enc := xml.NewEncoder(w)
	enc.Indent("", "\t")
	if err := enc.Encode(t); err != nil {
		return err
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n")
	return err
}
