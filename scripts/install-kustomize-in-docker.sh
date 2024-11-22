#!/bin/bash

# this script is a bit of a quick hack

set -euox pipefail

# this is set by docker build, fail if it isn't set 
TARGETARCH=${TARGETARCH}

# this should be propagated from the Makefile
KUSTOMIZE_VERSION=${KUSTOMIZE_VERSION}

TMPDIR=${TMPDIR:-/tmp}


REPO=https://github.com/kubernetes-sigs/kustomize/releases/download
TAG=kustomize%2F${KUSTOMIZE_VERSION}

TARBALL_FILENAME=kustomize_${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz

KUSTOMIZE_TARBALL=${TMPDIR}/${TARBALL_FILENAME}
KUSTOMIZE_TARBALL_URL=${REPO}/${TAG}/${TARBALL_FILENAME}
KUSTOMIZE_CHECKSUM=checksums.txt
KUSTOMIZE_CHECKSUM_URL=${REPO}/${TAG}/${KUSTOMIZE_CHECKSUM}

function cleanup() {
    rm -f "${KUSTOMIZE_TARBALL}"
    rm -f "${KUSTOMIZE_CHECKSUM}"
}
trap cleanup EXIT

echo "Downloading helm ${KUSTOMIZE_VERSION}"
curl -L -o ${KUSTOMIZE_TARBALL} ${KUSTOMIZE_TARBALL_URL}
curl -L -o ${TMPDIR}/${KUSTOMIZE_CHECKSUM} ${KUSTOMIZE_CHECKSUM_URL}

pushd ${TMPDIR}
echo "Verifying helm checksum"
grep ${TARBALL_FILENAME} ${KUSTOMIZE_CHECKSUM} | sha256sum -c
popd

tar -zxvf "${KUSTOMIZE_TARBALL}" -C "${TMPDIR}"

mkdir -p .output/third_party/kustomize
cp ${TMPDIR}/kustomize .output/third_party/kustomize/kustomize
