#!/bin/bash
set -xeuo pipefail
declare -A repos
# Pin versions for now for reproducibility
repos[ostreedev/ostree]=v2020.6
repos[coreos/rpm-ostree]=v2020.5
for repo in "${!repos[@]}"; do
    bn=$(basename ${repo})
    git clone --depth=1 -b ${repos[${repo}]} https://github.com/${repo} ${bn}
    # Our "standard" for ext-installed-tests
    cd ${bn}/tests/kolainst
    make -j 4
    make install
    cd -
    rm ${bn} -rf
done
