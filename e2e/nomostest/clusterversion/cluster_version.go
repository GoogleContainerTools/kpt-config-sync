// Copyright 2024 Google LLC
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

package clusterversion

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const clusterVersionPattern = `^(v?([0-9]+)(\.([0-9]+))?(\.([0-9]+))?([+-_].*)?)$`

// ClusterVersion represents the parts of a Kubernetes cluster version.
type ClusterVersion struct {
	Major  int
	Minor  int
	Patch  int
	Suffix string
}

// String returns the canonical string representation of a ClusterVersion.
func (cv ClusterVersion) String() string {
	if cv.Suffix != "" {
		return fmt.Sprintf("v%d.%d.%d%s",
			cv.Major, cv.Minor, cv.Patch, cv.Suffix)
	}
	return fmt.Sprintf("v%d.%d.%d", cv.Major, cv.Minor, cv.Patch)
}

// GKESuffix returns the GKE patch version integer from the -gke suffix
func (cv ClusterVersion) GKESuffix() (int, error) {
	if !strings.HasPrefix(cv.Suffix, "-gke.") {
		return -1, fmt.Errorf("missing -gke suffix")
	}
	val := strings.TrimPrefix(cv.Suffix, "-gke.")
	intVal, err := strconv.Atoi(val)
	if err != nil {
		return -1, fmt.Errorf("invalid suffix value: gke suffix must be an integer: %v", err)
	}
	return intVal, nil
}

// IsAtLeast returns whether the ClusterVersion is at least the provided version.
func (cv ClusterVersion) IsAtLeast(other ClusterVersion) bool {
	if cv.Major != other.Major {
		return cv.Major > other.Major
	}
	if cv.Minor != other.Minor {
		return cv.Minor > other.Minor
	}
	if cv.Patch != other.Patch {
		return cv.Patch > other.Patch
	}
	// Compare -gke suffix, if they exist
	myGKESuffix, _ := cv.GKESuffix()
	otherGKESuffix, _ := other.GKESuffix()
	if myGKESuffix != otherGKESuffix {
		return myGKESuffix > otherGKESuffix
	}
	// Base case - Equal
	return true
}

// ParseClusterVersion parses the string "gitVersion" of a Kubernetes cluster.
func ParseClusterVersion(version string) (ClusterVersion, error) {
	re := regexp.MustCompile(clusterVersionPattern)
	match := re.FindStringSubmatch(version)
	cv := ClusterVersion{}
	if match == nil {
		return cv, fmt.Errorf("invalid cluster version: %q does not match version regex: %q", version, clusterVersionPattern)
	}
	var err error
	cv.Major, err = strconv.Atoi(match[2])
	if err != nil {
		return cv, fmt.Errorf("invalid regex result: major version must be an integer: %v", err)
	}
	if len(match) > 4 && match[4] != "" {
		cv.Minor, err = strconv.Atoi(match[4])
		if err != nil {
			return cv, fmt.Errorf("Invalid regex result: minor version must be an integer: %v", err)
		}
	}
	if len(match) > 6 && match[6] != "" {
		cv.Patch, err = strconv.Atoi(match[6])
		if err != nil {
			return cv, fmt.Errorf("Invalid regex result: patch version must be an integer: %v", err)
		}
	}
	if len(match) > 7 && match[7] != "" {
		cv.Suffix = match[7]
	}
	return cv, nil
}
