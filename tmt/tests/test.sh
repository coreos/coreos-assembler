#!/bin/bash
set -eEuo pipefail
set -x

source "utils.sh"
trap collect_kola_artifacts ERR

if [ "$TEST_CASE" = "test-qemu" ]; then
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

    # reprovision test
    export KOLA_ACTION="run"
    export KOLA_ID="kola-reprovision"
    export KOLA_EXTRA_ARGS=(
      --tag=reprovision
    )
    run_kola
    collect_kola_artifacts

elif [ "$TEST_CASE" = "test-kola-upgrade" ]; then
    # upgrade test
    export KOLA_ACTION="run-upgrade"
    export KOLA_ID="run-upgrade"
    export KOLA_EXTRA_ARGS=(
      --upgrades
    )
    run_kola
    collect_kola_artifacts

elif [ "$TEST_CASE" = "test-kola-self" ]; then
    REPO_ROOT=$(cd ../.. && pwd)
    # self test
    export KOLA_ACTION="run"
    export KOLA_ID="kola-self"
    export KOLA_EXTRA_ARGS=(
        -E "${REPO_ROOT}/tests/kola-ci-self"
        'ext.kola-ci-self*'
    )
    run_kola
    collect_kola_artifacts
fi
