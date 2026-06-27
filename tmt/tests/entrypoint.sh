#!/bin/bash
set -euo pipefail

export TEST_CASE="$TEST_CASE"
case "$TEST_CASE" in
    "init")
        ./init.sh
        ;;
    "build-fcos"|"build-qemu")
        ./build.sh
        ;;
    "test-qemu"|"test-kola-upgrade"|"test-kola-self")
        ./test.sh
        ;;
    *)
        echo "Error: Test case '$TEST_CASE' not found!" >&2
        exit 1
        ;;
esac
