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

package oci

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"golang.org/x/net/context"
	"k8s.io/klog/v2"
)

const (
	// tmpLink is the temporary soft link name.
	tmpLink = "tmp-link"
)

// FetchPackage fetches the package from the OCI repository and write it to the destination.
func FetchPackage(ctx context.Context, imageName, ociRoot, rev string, auth authn.Authenticator) error {
	image, err := pullImage(imageName, remote.WithContext(ctx), remote.WithAuth(auth))
	if err != nil {
		return err
	}

	// Determine the digest of the image that was extracted
	imageDigestHash, err := image.Digest()
	if err != nil {
		return fmt.Errorf("failed to calculate image digest: %w", err)
	}

	destDir := filepath.Join(ociRoot, imageDigestHash.Hex)

	linkPath := filepath.Join(ociRoot, rev)
	oldDir, err := filepath.EvalSymlinks(linkPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to evaluate the symbolic path %q to the OCI package: %w", linkPath, err)
	}
	if oldDir == destDir {
		klog.Infof("no update required with the same image digest hash %q", imageDigestHash)
		return nil
	}

	if _, err = os.Stat(destDir); os.IsNotExist(err) {
		fileMode := os.FileMode(0755)
		if err = os.MkdirAll(destDir, fileMode); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", destDir, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check the directory %q: %w", destDir, err)
	}

	err = extract(image, destDir)
	if err != nil {
		return fmt.Errorf("failed to extract the image and write to the directory %q: %w", destDir, err)
	}

	klog.Infof("pulled image digest %q", imageDigestHash)
	return updateSymlink(ociRoot, linkPath, destDir, oldDir)
}

// pullImage pulls image from source using provided options for auth credentials
func pullImage(imageName string, options ...remote.Option) (v1.Image, error) {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %q: %v", imageName, err)
	}

	image, err := remote.Image(ref, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to pull image %s: %v", imageName, err)
	}
	return image, nil
}

// extract extracts (untar) image files to target directory.
func extract(image v1.Image, dir string) error {
	// Stream image files as if single tar (merged layers)
	ioReader := mutate.Extract(image)
	defer func() {
		if err := ioReader.Close(); err != nil {
			klog.Warningf("failed to close ioReader: %v", err)
		}
	}()

	// Write contents to target dir
	tarReader := tar.NewReader(ioReader)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path := filepath.Join(dir, hdr.Name)
		switch {
		case hdr.FileInfo().IsDir():
			if err := os.MkdirAll(path, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		case hdr.Linkname != "":
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				klog.Warning(err)
			}
		default:
			file, err := os.OpenFile(path,
				os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
				os.FileMode(hdr.Mode),
			)
			if err != nil {
				return err
			}
			defer func() {
				if err := file.Close(); err != nil {
					klog.Warningf("failed to close file %q: %v", file.Name(), err)
				}
			}()

			_, err = io.Copy(file, tarReader)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// updateSymlink updates the symbolic link to the package directory.
func updateSymlink(ociRoot, linkAbsPath, packageDir, oldPackageDir string) error {
	tmpLinkPath := filepath.Join(ociRoot, tmpLink)

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
