#!/bin/bash
# Entrypoint run via OpenShift Prow (https://docs.ci.openshift.org/)
# that tests RHCOS (openshift/os).
set -xeuo pipefail
# https://github.com/kubernetes/test-infra/blob/master/prow/jobs.md
BRANCH=${PULL_BASE_REF:-main}
case ${BRANCH} in
    # For now; OpenShift hasn't done the master->main transition
    main|master) RHCOS_BRANCH=master;;
    rhcos-*) RHCOS_BRANCH=release-${BRANCH#rhcos-};;
    *) echo "Unhandled base ref: ${BRANCH}" 1>&2 && exit 1;;
esac

# Prow jobs don't support adding emptydir today
export COSA_SKIP_OVERLAY=1
# Create a temporary cosa workdir
cd "$(mktemp -d)"
cosa init -b "${BRANCH}" https://github.com/openshift/os
exec src/config/ci/prow-build-test-qemu.sh
