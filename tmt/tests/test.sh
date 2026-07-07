#!/bin/bash
set -eEuo pipefail
set -x

source "utils.sh"
# The collect_kola_artifacts is triggered whenever a test fails by registerting
# it as an ERR trap.
trap collect_kola_artifacts ERR

if [ "$TEST_CASE" = "test-kola-self" ]; then
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
