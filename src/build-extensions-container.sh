#!/bin/bash
#Used by cmd/build-extensions-container.go
#Find the RHCOS ociarchive.
set -euo pipefail
arch=$1
shift
filename=$1
shift
buildid=$1
shift
builddir="$PWD/builds/latest/${arch}"
ostree_ociarchive=$(ls "${builddir}"/*-ostree*.ociarchive)
# Build the image, replacing the FROM directive with the local image we have
(cd src/config
 set -x
 podman build --from oci-archive:"$ostree_ociarchive" --network=host --build-arg COSA=true --label version="$buildid" -t localhost/extensions-container -f extensions/Dockerfile .
)
# Call skopeo to export it from the container storage to an oci-archive.
(set -x
 skopeo copy containers-storage:localhost/extensions-container oci-archive:"$filename" )
