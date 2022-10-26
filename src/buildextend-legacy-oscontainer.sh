#!/usr/bin/env bash
# shellcheck disable=SC1091
set -euo pipefail
# Start VM and call buildah
. /usr/lib/coreos-assembler/cmdlib.sh
final_outfile=$(realpath "$1"); shift
prepare_build
# shellcheck disable=SC2154
tmp_outfile=${tmp_builddir}/legacy-oscontainer.ociarchive
runvm -chardev "file,id=ociarchiveout,path=${tmp_outfile}" \
    -device "virtserialport,chardev=ociarchiveout,name=ociarchiveout" -- \
    /usr/lib/coreos-assembler/buildextend-legacy-oscontainer.py \
        --output "/dev/virtio-ports/ociarchiveout" "$@"
/usr/lib/coreos-assembler/finalize-artifact "${tmp_outfile}" "${final_outfile}"
