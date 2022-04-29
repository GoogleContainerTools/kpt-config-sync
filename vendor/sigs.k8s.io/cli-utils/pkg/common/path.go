// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

const (
	stdinDash    = "-"
	tmpDirPrefix = "diff-cmd-config"
	fileRegexp   = "config-*.yaml"
)

func processPaths(paths []string) genericclioptions.FileNameFlags {
	// No arguments means we are reading from StdIn
	fileNameFlags := genericclioptions.FileNameFlags{}
	if len(paths) == 0 {
		fileNames := []string{stdinDash}
		fileNameFlags.Filenames = &fileNames
		return fileNameFlags
	}

	// Must be a single directory here; set recursive flag.
	t := true
	fileNameFlags.Filenames = &paths
	fileNameFlags.Recursive = &t
	return fileNameFlags
}

// DemandOneDirectoryOrStdin processes "paths" to ensure the
// single argument in the array is a directory. Returns FileNameFlags
// populated with the directory (recursive flag set), or
// the StdIn dash. An empty array gets treated as StdIn
// (adding dash to the array). Returns an error if more than
// one element in the array or the filepath is not a directory.
func DemandOneDirectory(paths []string) (genericclioptions.FileNameFlags, error) {
	result := genericclioptions.FileNameFlags{}
	if len(paths) == 1 {
		dirPath := paths[0]
		if !IsDir(dirPath) {
			return result, fmt.Errorf("argument '%s' is not but must be a directory", dirPath)
		}
	}
	if len(paths) > 1 {
		return result, fmt.Errorf(
			"specify exactly one directory path argument; rejecting %v", paths)
	}
	result = processPaths(paths)
	return result, nil
}

func IsDir(dir string) bool {
	if f, err := os.Stat(dir); err == nil {
		if f.Mode().IsDir() {
			return true
		}
	}
	return false
}

// ExpandPackageDir expands the one package directory entry in the flags to all
// the config file paths recursively. Excludes the inventory object (since
// this object is specially processed). Used for the diff command, so it will
// not always show a diff of the inventory object. Must be called AFTER
// DemandOneDirectory.
func ExpandPackageDir(f genericclioptions.FileNameFlags) (genericclioptions.FileNameFlags, error) {
	if len(*f.Filenames) != 1 {
		return f, fmt.Errorf("expand package directory should pass one package directory. "+
			"Passed the following paths: %v", f.Filenames)
	}
	_, configFilepaths, err := ExpandDir((*f.Filenames)[0])
	if err != nil {
		return f, err
	}
	f.Filenames = &configFilepaths
	return f, nil
}

// FilterInputFile copies the resource config on stdin into a file
// at the tmpDir, filtering the inventory object. It is the
// responsibility of the caller to clean up the tmpDir. Returns
// an error if one occurs.
func FilterInputFile(in io.Reader, tmpDir string) error {
	// Copy the config from "in" into a local temp file.
	dir, err := ioutil.TempDir("", tmpDirPrefix)
	if err != nil {
		return err
	}
	tmpFile, err := ioutil.TempFile(dir, fileRegexp)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	klog.V(6).Infof("Temp File: %s", tmpFile.Name())
	if _, err := io.Copy(tmpFile, in); err != nil {
		return err
	}
	// Read the config stored locally, parsing into RNodes
	r := kio.LocalPackageReader{PackagePath: dir}
	nodes, err := r.Read()
	if err != nil {
		return err
	}
	klog.V(6).Infof("Num read configs: %d", len(nodes))
	// Filter RNodes to remove the inventory object.
	filteredNodes := []*yaml.RNode{}
	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			continue
		}
		// If object has inventory label, skip it.
		labels := meta.Labels
		if _, exists := labels[InventoryLabel]; !exists {
			filteredNodes = append(filteredNodes, node)
		}
	}
	// Write the remaining configs into a file in the tmpDir
	w := kio.LocalPackageWriter{
		PackagePath:           tmpDir,
		KeepReaderAnnotations: false,
	}
	klog.V(6).Infof("Writing %d configs", len(filteredNodes))
	return w.Write(filteredNodes)
}

// ExpandDir takes a single package directory as a parameter, and returns
// the inventory template filepath and an array of config file paths. If no
// inventory template file, then the first return value is an empty string.
// Returns an error if one occurred while processing the paths.
func ExpandDir(dir string) (string, []string, error) {
	filepaths := []string{}
	r := kio.LocalPackageReader{PackagePath: dir}
	nodes, err := r.Read()
	if err != nil {
		return "", filepaths, err
	}
	var invFilepath string
	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			continue
		}
		path := meta.Annotations[kioutil.PathAnnotation]
		path = filepath.Join(dir, path)
		// If object has inventory label, skip it.
		labels := meta.Labels
		if _, exists := labels[InventoryLabel]; exists {
			if invFilepath == "" {
				invFilepath = path
			}
			continue
		}
		filepaths = append(filepaths, path)
	}
	return invFilepath, filepaths, nil
}
