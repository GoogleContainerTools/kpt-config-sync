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

package registryproviders

import (
	"fmt"
	"path/filepath"
	"strings"

	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/testshell"
)

// use an auto-incrementing index to create unique file names for tarballs
var imageIndex int

// BuildImage creates a new OCIImage object and associated tarball using the provided
// Repository. The contents of the git repository will be bundled into a tarball
// at the artifactDir. The resulting OCIImage object can be pushed to a remote
// registry using its Push method.
func BuildImage(artifactDir string, shell *testshell.TestShell, repository *gitproviders.Repository, provider RegistryProvider) (*OCIImage, error) {
	commitHash, err := repository.Hash()
	if err != nil {
		return nil, fmt.Errorf("getting hash: %w", err)
	}
	branch, err := repository.CurrentBranch()
	if err != nil {
		return nil, fmt.Errorf("getting branch: %w", err)
	}
	// Use branch/hash for context and imageIndex to enforce file name uniqueness.
	// This avoids file name collision even if the test builds an image twice with
	// a dirty repo state.
	localSourceTgzPath := filepath.Join(artifactDir,
		fmt.Sprintf("%s-%s-%d.tgz", branch, commitHash, imageIndex))
	imageIndex++
	if _, err := shell.ExecWithDebug("tar", "-cvzf", localSourceTgzPath, repository.Root); err != nil {
		return nil, fmt.Errorf("packaging image: %w", err)
	}
	image := &OCIImage{
		localSourceTgzPath: localSourceTgzPath,
		Name:               repository.Name,
		syncURL:            provider.SyncURL(repository.Name),
		branch:             branch,
		shell:              shell,
		provider:           provider,
	}
	return image, nil
}

// OCIImage represents an OCI image that is pushed to a remote registry by the
// test scaffolding. It uses git references as version tags to enable straightforward
// integration with the git e2e tooling and to mimic how a user might leverage
// git and OCI.
type OCIImage struct {
	localSourceTgzPath string
	syncURL            string
	branch             string
	Name               string
	Digest             string
	shell              *testshell.TestShell
	provider           RegistryProvider
}

// FloatingBranchTag returns the floating tag that initially points to this image.
// This tag is suitable for use on RSync object spec but not for interacting with
// the image from the test suite.
// This uses the git branch as a version, such that the tag will be updated
// whenever a new image is pushed from the git branch. This enables syncing
// to an OCI image similar to a git branch.
func (o *OCIImage) FloatingBranchTag() string {
	return fmt.Sprintf("%s:%s", o.syncURL, o.branch)
}

// DigestTag returns the image tag formed using the image digest.
// This tag is suitable for use on RSync object spec but not for interacting with
// the image from the test suite.
func (o *OCIImage) DigestTag() string {
	return fmt.Sprintf("%s@%s", o.syncURL, o.Digest)
}

// Push the image to the remote registry using the provided registry endpoint.
func (o *OCIImage) Push(registry string) error {
	imageTag := fmt.Sprintf("%s:%s", registry, o.branch)
	_, err := o.shell.ExecWithDebug("crane", "append",
		"-f", o.localSourceTgzPath,
		"-t", imageTag)
	if err != nil {
		return fmt.Errorf("pushing image: %w", err)
	}
	out, err := o.shell.ExecWithDebug("crane", "digest", imageTag)
	if err != nil {
		return fmt.Errorf("getting digest: %w", err)
	}
	o.Digest = strings.TrimSpace(string(out))
	return nil
}

// Delete the image from the remote registry using the provided registry endpoint.
func (o *OCIImage) Delete() error {
	// How to delete images varies by provider, so delegate deletion to the provider.
	return o.provider.deleteImage(o.Name, o.Digest)
}
