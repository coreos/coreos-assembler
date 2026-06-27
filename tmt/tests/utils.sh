#!/bin/bash
set -eEuo pipefail
set -x

export COREOS_ASSEMBLER_CONTAINER="$IMAGE_URL"
export COSA_DIR="$HOME/workspace/build"

cosa ()
{
    podman run --rm --security-opt=label=disable --privileged \
    -v "${COSA_DIR}:/srv" --device=/dev/kvm \
    --device=/dev/fuse --tmpfs=/tmp -v /var/tmp:/var/tmp --name=cosa "${COREOS_ASSEMBLER_CONTAINER}" "$@";
}
collect_kola_artifacts() {
    mkdir -p "$TMT_TEST_DATA"
    cd "${COSA_DIR}" && tar -C "${OUTPUT_DIR}" -c --xz "${KOLA_ID}" > "${KOLA_ID}-${TOKEN}.tar.xz"
    cd "${COSA_DIR}" && mv "${KOLA_ID}-${TOKEN}.tar.xz" "${TMT_TEST_DATA}/${KOLA_ID}-${TOKEN}.tar.xz"
}
run_kola(){
    OUTPUT_DIR=$(cd "${COSA_DIR}" && cosa shell -- mktemp -d tmp/kola-XXXX)
    TOKEN="$(uuidgen | cut -f1 -d -)"
    KOLA_ID="${KOLA_ID:-kola}"
    cd "${COSA_DIR}" && cosa kola "${KOLA_ACTION}" --build=latest --arch="$(arch)" --output-dir="${OUTPUT_DIR}/${KOLA_ID}" "${KOLA_EXTRA_ARGS[@]}"
}
