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

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"k8s.io/klog/v2"
)

const (
	// tmpLink is the temporary soft link name.
	tmpLink = "tmp-link"
)

// EnvString retrieves the string value of the environment variable named by the key.
// If the variable is not present, it returns default value.
func EnvString(key, def string) string {
	if env := os.Getenv(key); env != "" {
		return env
	}
	return def
}

// EnvBool retrieves the boolean value of the environment variable named by the key.
// If the variable is not present, it returns default value.
func EnvBool(key string, def bool) bool {
	if env := os.Getenv(key); env != "" {
		res, err := strconv.ParseBool(env)
		if err != nil {
			return def
		}

		return res
	}
	return def
}

// EnvInt retrieves the int value of the environment variable named by the key.
// If the variable is not present, it returns default value.
func EnvInt(key string, def int) int {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.ParseInt(env, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: invalid env value (%v): using default, key=%s, val=%q, default=%d\n", err, key, env, def)
			return def
		}
		return int(val)
	}
	return def
}

// EnvFloat retrieves the float value of the environment variable named by the key.
// If the variable is not present, it returns default value.
func EnvFloat(key string, def float64) float64 {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.ParseFloat(env, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: invalid env value (%v): using default, key=%s, val=%q, default=%f\n", err, key, env, def)
			return def
		}
		return val
	}
	return def
}

// WaitTime returns the wait time between syncs in time.Duration type.
func WaitTime(seconds float64) time.Duration {
	return time.Duration(int(seconds*1000)) * time.Millisecond
}

// UpdateSymlink updates the symbolic link to the package directory.
func UpdateSymlink(helmRoot, linkAbsPath, packageDir, oldPackageDir string) error {
	tmpLinkPath := filepath.Join(helmRoot, tmpLink)

	if err := os.Symlink(packageDir, tmpLinkPath); err != nil {
		return fmt.Errorf("unable to create symlink: %w", err)
	}

	if err := os.Rename(tmpLinkPath, linkAbsPath); err != nil {
		return fmt.Errorf("unable to replace symlink: %w", err)
	}

	if err := os.RemoveAll(oldPackageDir); err != nil {
		klog.Warningf("unable to remove the previously package directory %s: %v", oldPackageDir, err)
	}
	klog.Infof("symlink %q updates to %q", linkAbsPath, packageDir)
	return nil
}
