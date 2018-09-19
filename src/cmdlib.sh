# Shared shell script library

fatal() {
    echo "error: $@" 1>&2; exit 1
}

preflight() {
    # Verify we have all dependencies
    local deps=$(grep -v '^#' /usr/lib/coreos-assembler/deps.txt)
    if ! rpm -q ${deps} >/dev/null; then
        local missing=""
        for pkg in ${deps}; do
            if ! rpm -q "${pkg}" >/dev/null; then
                missing="$missing $pkg"
            fi
        done
        fatal "Failed to find expected dependency packages: $missing"
    fi

    if [ $(stat -f --printf="%T" .) = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to find /dev/kvm"
    fi

    if ! capsh --print | grep -q 'Current.*cap_sys_admin'; then
        fatal "This container must currently be run with --privileged"
    fi

    if ! sudo true; then
        fatal "The user must currently have sudo privileges"
    fi

    # permissions on /dev/kvm vary by (host) distro.  If it's
    # not writable, recreate it.
    if ! [ -w /dev/kvm ]; then
        sudo rm -f /dev/kvm
        sudo mknod /dev/kvm c 10 232
        sudo setfacl -m u:$USER:rw /dev/kvm
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
