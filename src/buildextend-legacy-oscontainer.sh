#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=src/cmdlib.sh
. /usr/lib/coreos-assembler/cmdlib.sh

# Start VM and call buildah
final_outfile=$(realpath "$1"); shift
IMAGE_TYPE=legacy-oscontainer
prepare_build
tmp_outfile=${tmp_builddir}/legacy-oscontainer.ociarchive
runvm -- /usr/lib/coreos-assembler/buildextend-legacy-oscontainer.py \
    --output "${tmp_outfile}" "$@"
/usr/lib/coreos-assembler/finalize-artifact "${tmp_outfile}" "${final_outfile}"
