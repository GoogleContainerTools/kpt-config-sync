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

package initialize

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/printers"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	v1repo "kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
)

var forceValue bool

func init() {
	flags.AddPath(Cmd)
	Cmd.Flags().BoolVar(&forceValue, "force", false,
		"write to directory even if nonempty, overwriting conflicting files")
}

// Cmd is the Cobra object representing the nomos init command
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a Anthos Configuration Management directory",
	Long: `Initialize a Anthos Configuration Management directory

Set up a working Anthos Configuration Management directory with a default Repo object, documentation,
and directories.

By default, does not initialize directories containing files. Use --force to
initialize nonempty directories.`,
	Example: `  nomos init
  nomos init --path=my/directory
  nomos init --path=/path/to/my/directory`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		return Initialize(flags.Path, forceValue)
	},
}

// Initialize initializes a Nomos directory
func Initialize(root string, force bool) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		err = os.MkdirAll(root, os.ModePerm)
		if err != nil {
			return errors.Wrapf(err, "unable to create dir %q", root)
		}
	} else if err != nil {
		return err
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootDir, err := cmpath.AbsoluteOS(abs)
	if err != nil {
		return err
	}

	if !force {
		err := checkEmpty(rootDir)
		if err != nil {
			return err
		}
	}

	repoDir := repoDirectoryBuilder{root: rootDir}
	repoDir.createFile("", readmeFile, rootReadmeContents)

	// Create system/
	repoDir.createDir(v1repo.SystemDir)
	repoDir.createSystemFile(readmeFile, systemReadmeContents)
	repoObj, err := defaultRepo()
	if err != nil {
		return err
	}

	err = util.WriteObject(&printers.YAMLPrinter{}, rootDir.OSPath(), repoObj)
	if err != nil {
		return err
	}

	// Create cluster/
	repoDir.createDir(v1repo.ClusterDir)

	// Create clusterregistry/
	repoDir.createDir(v1repo.ClusterRegistryDir)

	// Create namespaces/
	repoDir.createDir(v1repo.NamespacesDir)

	return repoDir.errors
}

func checkEmpty(dir cmpath.Absolute) error {
	files, err := ioutil.ReadDir(dir.OSPath())
	if err != nil {
		return errors.Wrapf(err, "error reading %q", dir)
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") {
			return errors.Errorf("passed dir %q has file %q; use --force to proceed", dir, file.Name())
		}
	}
	return nil
}

type repoDirectoryBuilder struct {
	root   cmpath.Absolute
	errors status.MultiError
}

func (d repoDirectoryBuilder) createDir(dir string) {
	newDir := filepath.Join(d.root.OSPath(), dir)
	err := os.Mkdir(newDir, os.ModePerm)
	if err != nil {
		d.errors = status.Append(d.errors, status.PathWrapError(err, newDir))
	}
}

func (d repoDirectoryBuilder) createFile(dir string, path string, contents string) {
	file, err := os.Create(filepath.Join(d.root.OSPath(), dir, path))
	if err != nil {
		d.errors = status.Append(d.errors, status.PathWrapError(err, path))
		return
	}
	_, err = file.WriteString(contents)
	if err != nil {
		d.errors = status.Append(d.errors, status.PathWrapError(err, path))
	}
}

func (d repoDirectoryBuilder) createSystemFile(path string, contents string) {
	d.createFile(v1repo.SystemDir, path, contents)
}
