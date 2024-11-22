#!/bin/bash

set -euox pipefail

# this is set by docker build, fail if it isn't set 
TARGETARCH=${TARGETARCH}

# this should be propagated from the Makefile
HELM_VERSION=${HELM_VERSION}

TMPDIR=${TMPDIR:-/tmp}


REPO=https://get.helm.sh

TARBALL_FILENAME=helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz

HELM_TARBALL=${TMPDIR}/${TARBALL_FILENAME}
HELM_TARBALL_URL=${REPO}/$TARBALL_FILENAME
HELM_CHECKSUM=${HELM_TARBALL}.sha256
HELM_CHECKSUM_URL=${HELM_TARBALL_URL}.sha256


function cleanup() {
    rm -f "${HELM_TARBALL}"
    rm -f "${HELM_CHECKSUM}"
}
trap cleanup EXIT

echo "Downloading helm ${HELM_VERSION}"
curl -L -o ${HELM_TARBALL} ${HELM_TARBALL_URL}
curl -L -o ${HELM_CHECKSUM} ${HELM_CHECKSUM_URL}

echo "Verifying helm checksum"
echo "$(cat "${HELM_CHECKSUM}")  ${HELM_TARBALL}" | sha256sum -c

tar -zxvf "${HELM_TARBALL}" -C "${TMPDIR}"

mkdir -p .output/third_party/helm
cp ${TMPDIR}/linux-${TARGETARCH}/helm .output/third_party/helm/helm
