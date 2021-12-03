#!/bin/bash
# Entrypoint run via OpenShift Prow (https://docs.ci.openshift.org/)
# that tests RHCOS (openshift/os).
set -xeuo pipefail
# PULL_BASE_REF: https://github.com/kubernetes/test-infra/blob/master/prow/jobs.md
# But prefer OPENSHIFT_BUILD_REFERENCE if available, which is more correct in
# rehearsal jobs: https://github.com/coreos/coreos-assembler/pull/2598
BRANCH=${OPENSHIFT_BUILD_REFERENCE:-${PULL_BASE_REF:-main}}
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
cosa init -b "${RHCOS_BRANCH}" https://github.com/openshift/os
exec src/config/ci/prow-build-test-qemu.sh
