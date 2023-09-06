#!/bin/bash
set -euo pipefail

output_tarball=$1; shift

repo=cache/repo
composejson=cache/repo/compose.json

rm -rf "${repo}"
ostree init --repo="${repo}" --mode=archive-z2

# we do need to pull at least the overlay bits over virtiofs, but it shouldn't be that
# many files
ostree refs --repo tmp/repo overlay --list | \
    xargs -r ostree pull-local --repo "${repo}" tmp/repo
# And import commit metadata for all refs; this will bring in the previous
# build if there is one. Ideally, we'd import the SELinux policy too to take
# advantage of https://github.com/coreos/rpm-ostree/pull/1704 but that's yet
# more files over virtiofs and our pipelines don't have cache anyway (and devs likely
# use the privileged path).
ostree refs --repo tmp/repo | \
    xargs -r ostree pull-local --commit-metadata-only --repo "${repo}" tmp/repo

# run rpm-ostree
"$@" --repo "${repo}" --write-composejson-to "${composejson}"

if [ ! -f "${composejson}" ]; then
    # no commit was produced; we're done
    exit 0
fi

tar -f "${output_tarball}" -C "${repo}" -c .