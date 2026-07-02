export COREOS_ASSEMBLER_CONTAINER="$IMAGE_URL"
export COSA_DIR="$HOME/workspace/build"

# Wrapper that runs any cosa command inside a privileged podman container.
# COSA_DIR is bind-mounted as /srv inside the container. KVM and FUSE devices
# are exposed for nested virtualization and OSTree overlay operations.
cosa ()
{
    podman run --rm --security-opt=label=disable --privileged \
    -v "${COSA_DIR}:/srv" --device=/dev/kvm \
    --device=/dev/fuse --tmpfs=/tmp -v /var/tmp:/var/tmp \
    --name=cosa "${COREOS_ASSEMBLER_CONTAINER}" "$@";
}

# Creates a compressed tarball from the contents of OUTPUT_DIR and moves it to
# TMT_TEST_DATA, which tmt uploads to Testing Farm as test artifacts.
# This function is called when the test succeeds or automatically via an ERR
# trap if the test fails.
collect_kola_artifacts() {
    if ! [ -v OUTPUT_DIR ]; then
        echo "Warning: OUTPUT_DIR not set, skipping" >&2
        return 0
    fi
    mkdir -p "$TMT_TEST_DATA"
    cd "${COSA_DIR}" || exit
    tar -C "${OUTPUT_DIR}" -c --xz "${KOLA_ID}" > "${KOLA_ID}-${TOKEN}.tar.xz"
    mv "${KOLA_ID}-${TOKEN}.tar.xz" "${TMT_TEST_DATA}/${KOLA_ID}-${TOKEN}.tar.xz"
}

# Wrapper around `cosa kola` that runs it with the user-supplied KOLA_ACTION 
# and KOLA_EXTRA_ARGS variables. 
# Each run has a unique TOKEN (UUID prefix).
# OUTPUT_DIR and TOKEN are used by collect_kola_artifacts().
run_kola(){
    OUTPUT_DIR=$(cd "${COSA_DIR}" && cosa shell -- mktemp -d tmp/kola-XXXX)
    TOKEN="$(uuidgen | cut -f1 -d -)"
    KOLA_ID="${KOLA_ID:-kola}"
    cd "${COSA_DIR}" && cosa kola "${KOLA_ACTION}" \
        --build=latest --arch="$(arch)" \
        --output-dir="${OUTPUT_DIR}/${KOLA_ID}" "${KOLA_EXTRA_ARGS[@]}"
}
