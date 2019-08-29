#!/bin/bash
# This is invoked by Dockerfile.buildroot
set -euo pipefail
dn=$(dirname "$0")
deps=$(grep -v '^#' "${dn}"/buildroot-reqs.txt)
echo "Installing requirements"
echo "${deps}" | xargs yum -y install
brs=$(grep -v '^#' "${dn}"/buildroot-buildreqs.txt)
echo "Installing build dependencies of primary packages"
echo "${brs}" | xargs yum -y builddep
echo 'Done!'
