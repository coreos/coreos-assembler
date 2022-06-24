#!/bin/bash
set -euo pipefail

output_tarball=$1; shift
output_composejson=$1; shift

tarball=cache/output.tar
composejson=cache/compose.json

repo=cache/repo
rm -rf "${repo}" "${composejson}"
ostree init --repo="${repo}" --mode=archive-z2

# we do need to pull at least the overlay bits over 9p, but it shouldn't be that
# many files
ostree refs --repo tmp/repo overlay --list | \
    xargs -r ostree pull-local --repo "${repo}" tmp/repo
# And import commit metadata for all refs; this will bring in the previous
# build if there is one. Ideally, we'd import the SELinux policy too to take
# advantage of https://github.com/coreos/rpm-ostree/pull/1704 but that's yet
# more files over 9p and our pipelines don't have cache anyway (and devs likely
# use the privileged path).
ostree refs --repo tmp/repo | \
    xargs -r ostree pull-local --commit-metadata-only --repo "${repo}" tmp/repo

# run rpm-ostree
"$@" --repo "${repo}" --write-composejson-to "${composejson}"

if [ ! -f "${composejson}" ]; then
    # no commit was produced; we're done
    exit 0
fi

tar -f "${tarball}" -C "${repo}" -c .

# this is key bit where we move the OSTree content over 9p
mv "${tarball}" "${output_tarball}"
mv "${composejson}" "${output_composejson}"
