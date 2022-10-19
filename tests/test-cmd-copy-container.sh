#!/bin/bash
set -xeuo pipefail

# NOTE: both destination repos must be empty before starting

SRC_REPO=quay.io/jlebon/fedora-coreos
DEST_REPO_QUAY=quay.io/jlebon/fedora-coreos-2
DEST_REPO_QUAY_AUTHFILE=dest.quay.auth.json
DEST_REPO_APPCI=registry.ci.openshift.org/coreos/jlebon-fedora-coreos-test
DEST_REPO_APPCI_AUTHFILE=dest.appci.auth.json

fatal() {
    echo "$@"
    exit 1
}

# copy to quay.io; auto-default to preserving manifest list
cosa copy-container --dest-authfile "${DEST_REPO_QUAY_AUTHFILE}" \
    --tag=stable --tag=stable-single "${SRC_REPO}" "${DEST_REPO_QUAY}"
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable" | grep -q manifests
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-amd64" && fatal "expected missing"
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-arm64" && fatal "expected missing"
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-s390x" && fatal "expected missing"
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-single" | grep -q layers

# copy to quay.io; force arch tag
cosa copy-container --dest-authfile "${DEST_REPO_QUAY_AUTHFILE}" \
    --tag=stable --manifest-list-to-arch-tag=always \
    "${SRC_REPO}" "${DEST_REPO_QUAY}"
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-amd64" | grep -q layers
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-arm64" | grep -q layers
skopeo inspect --raw "docker://${DEST_REPO_QUAY}:stable-s390x" | grep -q layers

# copy to registry.ci; auto-default to arch tag transform
cosa copy-container --dest-authfile "${DEST_REPO_APPCI_AUTHFILE}" \
    --tag=stable --tag=stable-single "${SRC_REPO}" "${DEST_REPO_APPCI}"
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable" && fatal "expected missing"
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-amd64" | grep -q layers
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-arm64" | grep -q layers
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-s390x" | grep -q layers
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-single" | grep -q layers

# copy in v2s2 mode
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-single" | grep -q vnd.oci.image.config.v1
cosa copy-container --dest-authfile "${DEST_REPO_APPCI_AUTHFILE}" \
    --tag=stable-single --v2s2 "${SRC_REPO}" "${DEST_REPO_APPCI}"
skopeo inspect --raw "docker://${DEST_REPO_APPCI}:stable-single" | grep -q vnd.docker.distribution.manifest.v2
