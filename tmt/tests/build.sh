#!/bin/bash
set -eo pipefail
set -x

source "utils.sh"

if [ "$TEST_CASE" = "build-fcos" ]; then
    cosa build
elif [ "$TEST_CASE" = "build-qemu" ]; then
    cosa osbuild qemu
fi
