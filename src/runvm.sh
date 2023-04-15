#!/bin/bash
set -euo pipefail

# This script just loads cmdlib.sh and executes runvm() with the given
# command line arguemnts to the script. It's used as a convenience
# wrapper for calling into runvm from other languages (i.e. python).

dn=$(dirname "$0")
# shellcheck source=src/cmdlib.sh
. "${dn}"/cmdlib.sh

runvm "$@"
