# Shared shell script library

fatal() {
    echo "error: $@" 1>&2; exit 1
}

preflight() {
    # Verify we have all dependencies
    local deps=$(grep -v '^#' /usr/lib/coreos-assembler/deps.txt)
    # Explicitly check the packages in one rpm -q to avoid
    # overhead, only drop down to individual calls if that fails.
    # We use --whatprovides so we handle file paths too.
    if ! rpm -q --whatprovides ${deps} &>/dev/null; then
        local missing=""
        for dep in ${deps}; do
            if ! rpm -q --whatprovides "${dep}" &>/dev/null; then
                missing="$missing $dep"
            fi
        done
        fatal "Failed to find expected dependencies:$missing"
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

    # This dir is no longer used
    rm builds/work -rf

    # Allocate temporary space for this build
    tmp_builddir=${workdir}/tmp/build
    rm ${tmp_builddir} -rf
    mkdir ${tmp_builddir}
    # And everything after this assumes it's in the temp builddir
    cd ${tmp_builddir}
}

# We'll rewrite this in a real language I promise
manifest_get() {
    python3 -c 'import sys,yaml; print(yaml.safe_load(open(sys.argv[1]))'"$1"')' "${manifest}"
}

runcompose() {
    local treecompose_args=""
    if ! grep -q '^# disable-unified-core' "${manifest}"; then
        treecompose_args="${treecompose_args} --unified-core"
    fi
    sudo rpm-ostree compose tree --repo=${workdir}/repo-build --cachedir=${workdir}/cache ${treecompose_args} \
         ${TREECOMPOSE_FLAGS:-} ${manifest} "$@"
}
