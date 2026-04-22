#!/bin/bash
set -eEuo pipefail
set -x

source "utils.sh"

CONFIG_GIT_URL="https://github.com/coreos/fedora-coreos-config"
CONFIG_GIT_REF="testing-devel"

echo "cosa container: $COREOS_ASSEMBLER_CONTAINER"
echo "arch: $(arch)"

echo "git version: $(git --version)"
echo "git url: ${CONFIG_GIT_URL}"
echo "git branch: ${CONFIG_GIT_REF}"

mkdir -p "$COSA_DIR"
cosa init --force "${CONFIG_GIT_URL}" --branch "${CONFIG_GIT_REF}"
