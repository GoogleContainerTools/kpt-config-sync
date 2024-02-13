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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/kyaml/copyutil"
)

// use an auto-incrementing index to create unique file names for tarballs
var imageIndex int

type imageOptions struct {
	version       string
	sourcePackage string
	objects       []client.Object
	scheme        *runtime.Scheme
}

// ImageOption is an optional parameter when building an OCI image.
type ImageOption func(options *imageOptions)

// ImageVersion builds the image with the specified version.
func ImageVersion(version string) func(options *imageOptions) {
	return func(options *imageOptions) {
		options.version = version
	}
}

// ImageSourcePackage builds the image with the specified package name.
// It should be a subfolder under '../testdata/hydration'.
func ImageSourcePackage(sourcePackage string) func(options *imageOptions) {
	return func(options *imageOptions) {
		options.sourcePackage = sourcePackage
	}
}

// ImageInputObjects builds the image with the specified objects.
// A scheme must be provided to encode the object.
func ImageInputObjects(scheme *runtime.Scheme, objs ...client.Object) func(options *imageOptions) {
	return func(options *imageOptions) {
		options.scheme = scheme
		options.objects = objs
	}
}

// BuildImage creates a new OCIImage object and associated tarball using the provided
// Repository. The contents of the git repository will be bundled into a tarball
// at the artifactDir. The resulting OCIImage object can be pushed to a remote
// registry using its Push method.
func BuildImage(artifactDir string, shell *testshell.TestShell, provider RegistryProvider, rsRef types.NamespacedName, opts ...ImageOption) (*OCIImage, error) {
	options := imageOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	// Use latest as a floating tag when a version is not specified
	version := options.version
	if version == "" {
		version = "latest"
	}
	packageName := options.sourcePackage
	if options.sourcePackage == "" {
		packageName = "test"
	}

	// Use package name/version for context and imageIndex to enforce file name uniqueness.
	// This avoids file name collision even if the test builds an image twice with
	// a dirty repo state.
	tmpDir := filepath.Join(artifactDir, packageName, version, strconv.Itoa(imageIndex))
	if options.sourcePackage != "" {
		inputDir := "../testdata/hydration/" + options.sourcePackage
		if err := copyutil.CopyDir(inputDir, tmpDir); err != nil {
			return nil, fmt.Errorf("copying package directory: %v", err)
		}
	}
	for _, obj := range options.objects {
		fullPath := filepath.Join(tmpDir, fmt.Sprintf("%s-%s-%s-%s-%s.yaml",
			obj.GetObjectKind().GroupVersionKind().Group,
			obj.GetObjectKind().GroupVersionKind().Version,
			obj.GetObjectKind().GroupVersionKind().Kind,
			obj.GetNamespace(), obj.GetName()))
		bytes, err := testkubeclient.SerializeObject(obj, ".yaml", options.scheme)
		if err != nil {
			return nil, err
		}
		if err = testkubeclient.WriteToFile(fullPath, bytes); err != nil {
			return nil, err
		}
	}

	// Ensure tmpDir always exists, even if it is an empty package.
	if err := os.MkdirAll(tmpDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("creating tmp dir: %w", err)
	}
	localSourceTgzPath := filepath.Join(artifactDir, fmt.Sprintf("%s-%s-%d.tgz", packageName, version, imageIndex))
	imageIndex++
	if _, err := shell.ExecWithDebug("tar", "-cvzf", localSourceTgzPath, "-C", tmpDir, "."); err != nil {
		return nil, fmt.Errorf("packaging image: %w", err)
	}
	image := &OCIImage{
		localSourceTgzPath: localSourceTgzPath,
		Name:               rsRef.String(),
		syncURL:            provider.SyncURL(rsRef.String()),
		version:            version,
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
	version            string
	Name               string
	Digest             string
	shell              *testshell.TestShell
	provider           RegistryProvider
}

// FloatingTag returns the floating tag that initially points to this image.
// This tag is suitable for use on RSync object spec but not for interacting with
// the image from the test suite.
func (o *OCIImage) FloatingTag() string {
	return fmt.Sprintf("%s:%s", o.syncURL, o.version)
}

// DigestTag returns the image tag formed using the image digest.
// This tag is suitable for use on RSync object spec but not for interacting with
// the image from the test suite.
func (o *OCIImage) DigestTag() string {
	return fmt.Sprintf("%s@%s", o.syncURL, o.Digest)
}

// Push the image to the remote registry using the provided registry endpoint.
func (o *OCIImage) Push(registry string) error {
	imageTag := fmt.Sprintf("%s:%s", registry, o.version)
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
