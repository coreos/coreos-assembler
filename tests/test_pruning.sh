#!/bin/bash
set -xeuo pipefail

# This test isn't standalone for now. It's executed from the `.cci.jenkinsfile`.
# It expects to be running in cosa workdir where a single fresh build was made.

# Test that first build has been pruned
cosa build ostree --force-image # this is a trick to get a no-op new build
cosa build ostree --force-image
cosa build ostree --force-image
jq -e '.builds|length == 3' builds/builds.json
jq -e '.builds[2].id | endswith("0-1")' builds/builds.json

# Test --skip-prune
cosa build ostree --force-image --skip-prune
jq -e '.builds|length == 4' builds/builds.json
jq -e '.builds[3].id | endswith("0-1")' builds/builds.json

# Test prune --dry-run
cosa prune --dry-run
jq -e '.builds|length == 4' builds/builds.json
jq -e '.builds[3].id | endswith("0-1")' builds/builds.json

# Test --keep-last-n=0 skips pruning
cosa prune --keep-last-n=0
jq -e '.builds|length == 4' builds/builds.json
jq -e '.builds[3].id | endswith("0-1")' builds/builds.json

# Test prune --keep-last-n=1
cosa prune --keep-last-n=1
jq -e '.builds|length == 1' builds/builds.json
jq -e '.builds[0].id | endswith("0-4")' builds/builds.json
