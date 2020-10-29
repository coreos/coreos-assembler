#!/bin/bash
set -xue
tmpd="$(mktemp -p /tmp -d ci-XXX)"
trap "rm -rf ${tmpd}" EXIT

curd="$(git rev-parse --show-toplevel)"
ref="$(git rev-parse HEAD)"

pushd "${tmpd}"
rsync -a "${curd}/" "${tmpd}/cosa"
find ${tmpd}
pushd "${tmpd}/cosa"

git remote add citest https://github.com/coreos/coreos-assembler
git fetch citest master
upref="$(git rev-parse citest/master)"

failed=0
for i in entrypoint mantle;
do
    pushd "${tmpd}/cosa/$i"
    golangci-lint run --timeout=10m --new-from-rev="${upref}" || failed=1
done

if [[ "${failed:-0}" -ne 0 ]]; then
    cat <<EOM
************************************************************

            GOLANG LINTING HAS FAILED
    golangci-lint tests all new code commits.

You can vet your GoLang changes locally in COSA running:

    golangci-lint run --timeout=3m --new-from-rev=${upref}

Or run this script after committing localy using:
    make golint


***********************************************************
EOM
    exit 1;
fi

exit 0
