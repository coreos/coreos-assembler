#!/bin/bash
# Builds CoreOS image artifacts. TEST_CASE selects the build stage:
#
#   build-fcos  - Full OS build: fetches RPMs and composes the OSTree commit
#                 and base image artifacts into COSA_DIR/builds/.
#
set -eo pipefail
set -x

source "utils.sh"

if [ "$TEST_CASE" = "build-fcos" ]; then
    cosa build
fi
