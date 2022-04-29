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

package filesystem

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

// WalkDirectory exported for testing.
var WalkDirectory = walkDirectory

// walkDirectory walks a directory and returns a list of all dirs / files / errors.
func walkDirectory(dir string) ([]string, error) {
	var seen []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			seen = append(seen, fmt.Sprintf("path=%s error=%s", path, err))
			return nil
		}
		seen = append(seen, fmt.Sprintf("path=%s mode=%o size=%d mtime=%s", path, info.Mode(), info.Size(), info.ModTime()))
		// Skip .git subdirectories.
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		return nil
	})
	return seen, err
}

// logWalkDirectory logs a directory walk to klog.Error
func logWalkDirectory(dir string) {
	files, err := walkDirectory(dir)
	if err != nil {
		klog.Errorf("error while walking %s: %s", dir, err)
	}
	klog.Errorf("walked %d files in %s", len(files), dir)
	for _, file := range files {
		klog.Errorf("  %s", file)
	}
}
