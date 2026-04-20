#!/bin/bash
set -euo pipefail

# IMAGE_URL is the coreos-assembler image that is built by konflux for each run.
export COREOS_ASSEMBLER_CONTAINER="$IMAGE_URL"
export COSA_DIR=$HOME/workspace/build
cosa ()
{
    set -x
    podman run --rm --security-opt=label=disable --privileged \
    -v="${COSA_DIR}":/srv/ --device=/dev/kvm \
    --device=/dev/fuse --tmpfs=/tmp -v=/var/tmp:/var/tmp --name=cosa "${COREOS_ASSEMBLER_CONTAINER}" "$@";
}

cosa init --force https://github.com/coreos/fedora-coreos-config --branch testing-devel
# Test if the newly built container can build the fcos image
cosa build
