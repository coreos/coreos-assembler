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
specs=$(grep -v '^#' "${dn}"/buildroot-specs.txt)
echo "Installing build dependencies from canonical spec files"
tmpd=$(mktemp -d) && trap 'rm -rf ${tmpd}' EXIT
(cd "${tmpd}" && echo "${specs}" | xargs curl -L --remote-name-all)
(cd "${tmpd}" && find . -type f -print0 | xargs -0 yum -y builddep --spec)
echo 'Done!'
