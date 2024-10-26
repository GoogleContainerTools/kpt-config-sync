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
	"sigs.k8s.io/kustomize/kyaml/filesys"
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
// It should be a subfolder under '../testdata/'.
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
func BuildImage(artifactDir string, shell *testshell.TestShell, provider OCIRegistryProvider, rsRef types.NamespacedName, opts ...ImageOption) (*OCIImage, error) {
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
		inputDir := "../testdata/" + options.sourcePackage
		if err := copyutil.CopyDir(filesys.MakeFsOnDisk(), inputDir, tmpDir); err != nil {
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
	imageName := rsRef.String()
	image := &OCIImage{
		LocalSourceTgzPath: localSourceTgzPath,
		Name:               imageName,
		Tag:                version,
		Provider:           provider,
	}
	return image, nil
}

// OCIImage represents an OCI image that is pushed to a remote registry by the
// test scaffolding. It uses git references as version tags to enable straightforward
// integration with the git e2e tooling and to mimic how a user might leverage
// git and OCI.
type OCIImage struct {
	LocalSourceTgzPath string
	Tag                string // aka Version
	Name               string
	Digest             string
	Provider           OCIRegistryProvider
}

// OCIImageID returns the ID of the OCI image.
func (o *OCIImage) OCIImageID() OCIImageID {
	return OCIImageID{
		Registry:   o.Provider.RegistryRemoteAddress(),
		Repository: o.Provider.RepositoryPath(),
		Name:       o.Name,
		Tag:        o.Tag,
		Digest:     o.Digest,
	}
}

// RemoteAddressWithTag returns the image address with version tag.
// For pulling with RSync's `.spec.oci.image`
func (o *OCIImage) RemoteAddressWithTag() (string, error) {
	return fmt.Sprintf("%s:%s", o.Provider.ImageRemoteAddress(o.Name), o.Tag), nil
}

// RemoteAddressWithDigest returns the image address with digest.
// For pulling with RSync's `.spec.oci.image`
func (o *OCIImage) RemoteAddressWithDigest() (string, error) {
	return fmt.Sprintf("%s@%s", o.Provider.ImageRemoteAddress(o.Name), o.Digest), nil
}

// LocalAddressWithDigest returns the local image address with digest.
// For accessing the image locally, for example, signing the image.
func (o *OCIImage) LocalAddressWithDigest() (string, error) {
	localAddress, err := o.Provider.ImageLocalAddress(o.Name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s", localAddress, o.Digest), nil
}

// Delete the image from the remote registry using the provided registry endpoint.
func (o *OCIImage) Delete() error {
	// How to delete images varies by provider, so delegate deletion to the provider.
	return o.Provider.DeleteImage(o.Name, o.Digest)
}

// ociURL prepends the `oci://` scheme onto the provided address.
func ociURL(address string) string {
	return fmt.Sprintf("oci://%s", address)
}

// OCIImageID represents the minimum information required to pull an OCI image.
type OCIImageID struct {
	// Registry address (optional, default dockerhub) without the repository or
	// scheme.
	Registry string
	// Repository name or path (optional) used for namespacing and hierarchy.
	Repository string
	// Name of the image.
	Name string
	// Tag of the image (optional, default latest).
	Tag string
	// Digest of the image (optional), including the "sha256:"" prefix.
	Digest string
}

// Address returns the address of the image
// [REGISTRY/][REPOSITORY/]NAME[:TAG][@DIGEST],
// WITHOUT the "oci://" scheme.
func (id OCIImageID) Address() string {
	sb := strings.Builder{}
	if id.Registry != "" {
		sb.WriteString(id.Registry)
		sb.WriteRune('/')
	}
	if id.Repository != "" {
		sb.WriteString(id.Repository)
		sb.WriteRune('/')
	}
	sb.WriteString(id.Name)
	if id.Tag != "" {
		sb.WriteRune(':')
		sb.WriteString(id.Tag)
	}
	if id.Digest != "" {
		sb.WriteRune('@')
		sb.WriteString(id.Digest)
	}
	return sb.String()
}

// URL returns the URL of the image
// SCHEME://[REGISTRY/][REPOSITORY/]NAME[:TAG][@DIGEST],
// WITH the "oci://" scheme.
func (id OCIImageID) URL() string {
	return ociURL(id.Address())
}

// String returns the URL of the image.
func (id OCIImageID) String() string {
	return id.URL()
}

// WithoutDigest returns a copy of the OCIImageID without the digest.
func (id OCIImageID) WithoutDigest() OCIImageID {
	return OCIImageID{
		Registry:   id.Registry,
		Repository: id.Repository,
		Name:       id.Name,
		Tag:        id.Tag,
	}
}

// WithoutTag returns a copy of the OCIImageID without the tag.
func (id OCIImageID) WithoutTag() OCIImageID {
	return OCIImageID{
		Registry:   id.Registry,
		Repository: id.Repository,
		Name:       id.Name,
		Digest:     id.Digest,
	}
}

// DigestWithoutPrefix returns the image digest without the sha256 prefix
func (id OCIImageID) DigestWithoutPrefix() string {
	if strings.HasPrefix(id.Digest, "sha256:") {
		return strings.TrimPrefix(id.Digest, "sha256:")
	}
	return id.Digest
}
