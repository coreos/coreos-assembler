#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=src/cmdlib.sh
. /usr/lib/coreos-assembler/cmdlib.sh

# Start VM and call buildah
final_outfile=$(realpath "$1"); shift
if [ "$(uname -m)" == 'ppc64le' ]; then
    # helps with 9p 'cannot allocate memory' errors on ppc64le
    COSA_SUPERMIN_MEMORY=6144
fi
IMAGE_TYPE=legacy-oscontainer
prepare_build
tmp_outfile=${tmp_builddir}/legacy-oscontainer.ociarchive
runvm -chardev "file,id=ociarchiveout,path=${tmp_outfile}" \
    -device "virtserialport,chardev=ociarchiveout,name=ociarchiveout" -- \
    /usr/lib/coreos-assembler/buildextend-legacy-oscontainer.py \
        --output "/dev/virtio-ports/ociarchiveout" "$@"
/usr/lib/coreos-assembler/finalize-artifact "${tmp_outfile}" "${final_outfile}"
