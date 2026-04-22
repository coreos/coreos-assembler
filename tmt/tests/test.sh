#!/bin/bash
set -eEuo pipefail
set -x

source "utils.sh"
# The collect_kola_artifacts is triggered whenever a test fails by registerting
# it as an ERR trap.
trap collect_kola_artifacts ERR

if [ "$TEST_CASE" = "test-qemu" ]; then
    # run all tests except those tagged "reprovision" Failed tests are retried once;
    export KOLA_ACTION="run"
    export KOLA_ID="kola"
    export KOLA_EXTRA_ARGS=(
      --rerun
      --allow-rerun-success=tags=needs-internet
      --on-warn-failure-exit-77
      --tag=!reprovision
      --parallel=5
    )
    run_kola
    collect_kola_artifacts

    # reprovision tests
    export KOLA_ACTION="run"
    export KOLA_ID="kola-reprovision"
    export KOLA_EXTRA_ARGS=(
      --tag=reprovision
    )
    run_kola
    collect_kola_artifacts

elif [ "$TEST_CASE" = "test-kola-upgrade" ]; then
    # Boots the previous release, applies the new image as an upgrade, and verifies
    # the system comes up correctly.
    export KOLA_ACTION="run-upgrade"
    export KOLA_ID="run-upgrade"
    export KOLA_EXTRA_ARGS=(
      --upgrades
    )
    run_kola
    collect_kola_artifacts

elif [ "$TEST_CASE" = "test-kola-self" ]; then
    # Copies the self-test suite into COSA_DIR so the container can reach it via /srv,
    # then runs only ext.kola-ci-self* tests against that external test directory.
    REPO_ROOT=$(cd ../.. && pwd)
    cp -r "${REPO_ROOT}/tests/kola-ci-self" "${COSA_DIR}/kola-ci-self"
    export KOLA_ACTION="run"
    export KOLA_ID="kola-self"
    export KOLA_EXTRA_ARGS=(
        -E "/srv/kola-ci-self"
        'ext.kola-ci-self*'
    )
    run_kola
    collect_kola_artifacts
fi
