# Shared shell script library

fatal() {
    echo "error: $@" 1>&2; exit 1
}

preflight() {
    if [ $(stat -f --printf="%T" .) = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to access /dev/kvm"
    fi

    if ! capsh --print | grep -q 'Current.*cap_sys_admin'; then
        fatal "This container must currently be run with --privileged"
    fi
}

prepare_build() {
    preflight
    if ! [ -d repo ]; then
        fatal "No $(pwd)/repo found; did you run coreos-assembler init?"
    fi

    export workdir=$(pwd)
    export configdir=${workdir}/src/config
    export manifest=${configdir}/manifest.yaml

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    echo "Using manifest: ${manifest}"

    # Abuse the rojig/name as the name of the VM images
    export name=$(manifest_get '["rojig"]["name"]')
    # TODO - allow this to be unset
    export ref=$(manifest_get '["ref"]')

    cd builds
    rm -rf work
    mkdir -p work
}

# We'll rewrite this in a real language I promise
manifest_get() {
    python3 -c 'import sys,yaml; print(yaml.safe_load(open(sys.argv[1]))'"$1"')' "${manifest}"
}

runcompose() {
    local treecompose_args=""
    if grep -q '^# unified-core' "${manifest}"; then
        treecompose_args="${treecompose_args} --unified-core"
    fi
    sudo rpm-ostree compose tree --repo=${workdir}/repo-build --cachedir=${workdir}/cache ${treecompose_args} \
         ${TREECOMPOSE_FLAGS:-} ${manifest} "$@"
}
