// Copyright 2023 Google LLC
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

package iam

import (
	"fmt"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
)

// ValidateServiceAccountExists validates that the specified GSA exists in the
// test project.
func ValidateServiceAccountExists(nt *nomostest.NT, gsaEmail string) error {
	if _, err := DescribeServiceAccount(nt, gsaEmail, *e2e.GCPProject); err != nil {
		return fmt.Errorf("describing service account %q: %w", gsaEmail, err)
	}
	return nil
}

// DescribeServiceAccount returns a human readable description of a GCP service
// account, if it exists.
func DescribeServiceAccount(nt *nomostest.NT, gsaEmail, projectID string) ([]byte, error) {
	out, err := nt.Shell.ExecWithDebug("gcloud", "iam", "service-accounts", "describe", gsaEmail, "--project", projectID)
	if err != nil {
		return nil, fmt.Errorf("describing GCP service account: %w", err)
	}
	return out, nil
}
