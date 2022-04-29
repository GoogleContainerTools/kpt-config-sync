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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

const delimiter = "---\n"

var destination = flag.String("destination", "", "Path to the destination file")

func filePaths(path string) ([]string, error) {
	var paths []string

	err := filepath.Walk(path, func(fPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Walk is recursive by default, but this makes append_manifests harder to use.  To get
		// around this, we don't let it walk any directory but the one that was the input.
		if info.IsDir() {
			if fPath != path {
				// filepath.SkipDir is a special variable used to prevent the walking of a directory
				return filepath.SkipDir
			}
			return nil
		}

		paths = append(paths, fPath)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func main() {
	flag.Parse()
	if *destination == "" {
		panic("Must specify a desination path")
	}

	// Ingest input files/dirs as positional arguments
	var paths []string
	for _, inputPath := range flag.Args() {
		fps, err := filePaths(inputPath)
		if err != nil {
			panic(err)
		}
		paths = append(paths, fps...)
	}

	// Append each of the paths to the yamlResources file
	var yamlResources []string
	for _, p := range paths {
		readBytes, err := ioutil.ReadFile(p)
		if err != nil {
			panic(err)
		}

		// If a file doesn't end in a newline, add one
		if !strings.HasSuffix(string(readBytes), "\n") {
			readBytes = append(readBytes, []byte("\n")...)
		}

		yamlResources = append(yamlResources, string(readBytes))
	}

	// overwrite the file with our new contents
	byteOut := []byte(strings.Join(yamlResources, delimiter))
	err := ioutil.WriteFile(*destination, byteOut, 0644)
	if err != nil {
		panic(err)
	}
}
