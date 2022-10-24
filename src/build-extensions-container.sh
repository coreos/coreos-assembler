#!/bin/bash
set -euo pipefail

# Used by cmd/build-extensions-container.go. Runs via `runvm` via `cosash.go`.

arch=$1; shift
filename=$1; shift
buildid=$1; shift

workdir=$PWD
builddir="${workdir}/builds/latest/${arch}"
ostree_ociarchive=$(ls "${builddir}"/*-ostree*.ociarchive)

if [ ! -d "${workdir}/src/yumrepos" ]; then
    ctx_dir=${workdir}/src/config
else
    ctx_dir="${workdir}/tmp/build-extensions-container"
    rm -rf "${ctx_dir}"
    cp -a "${workdir}/src/config" "${ctx_dir}"
    find "${workdir}/src/yumrepos/" -maxdepth 1 -type f -name '*.repo' -exec cp -t "${ctx_dir}" {} +
    cleanup_ctx_dir=1
fi

# Build the image, replacing the FROM directive with the local image we have
img=localhost/extensions-container
(set -x; podman build --from oci-archive:"$ostree_ociarchive" --network=host \
    --build-arg COSA=true --label version="$buildid" \
    -t "${img}" -f extensions/Dockerfile "${ctx_dir}")

if [ -n "${cleanup_ctx_dir:-}" ]; then
    rm -rf "${ctx_dir}"
fi

# Call skopeo to export it from the container storage to an oci-archive.
(set -x; skopeo copy "containers-storage:${img}" oci-archive:"$filename")
