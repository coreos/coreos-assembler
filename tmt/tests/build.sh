#!/bin/bash
# Builds CoreOS image artifacts. TEST_CASE selects the build stage:
#
#   build-fcos  - Full OS build: fetches RPMs and composes the OSTree commit
#                 and base image artifacts into COSA_DIR/builds/.
#
#   build-qemu  - Produces a QEMU-bootable qcow2 disk image from the artifacts
#                 created by build-fcos. Required before any kola QEMU tests run.
set -eo pipefail
set -x

source "utils.sh"

if [ "$TEST_CASE" = "build-fcos" ]; then
    cosa build
elif [ "$TEST_CASE" = "build-qemu" ]; then
    cosa osbuild qemu
fi
