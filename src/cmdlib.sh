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

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p ${workdir}/tmp

    # Needs to be absolute for rpm-ostree today
    export changed_stamp=$(pwd)/tmp/treecompose.changed

    # Allocate temporary space for this build
    tmp_builddir=${workdir}/tmp/build
    rm ${tmp_builddir} -rf
    mkdir ${tmp_builddir}
    # And everything after this assumes it's in the temp builddir
    cd ${tmp_builddir}
    # *This* tmp directory is truly temporary to this build, and
    # contains artifacts we definitely don't want to outlive it, unlike
    # other things in ${workdir}/tmp.  We also export it as an environment
    # variable for child processes like gf-oemid.
    mkdir tmp
    export TMPDIR=$(pwd)/tmp
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
    # Implement support for automatic local overrides:
    # https://github.com/coreos/coreos-assembler/issues/118
    local overridesdir=${workdir}/overrides/
    if [ -d ${overridesdir}/rpm ]; then
        (cd ${overridesdir}/rpm && createrepo_c .)
        echo "Using RPM overrides from: ${overridesdir}/rpm"
        local tmp_overridesdir=${TMPDIR}/override
        mkdir ${tmp_overridesdir}
        cat > ${tmp_overridesdir}/coreos-assembler-override-manifest.yaml <<EOF
include: ${workdir}/src/config/manifest.yaml
repos:
  - coreos-assembler-local-overrides
EOF
        # Because right now rpm-ostree doesn't look for .repo files in
        # each included dir.
        # https://github.com/projectatomic/rpm-ostree/issues/1628
        cp ${workdir}/src/config/*.repo ${tmp_overridesdir}/
        cat > ${tmp_overridesdir}/coreos-assembler-local-overrides.repo <<EOF
[coreos-assembler-local-overrides]
name=coreos-assembler-local-overrides
baseurl=file://${workdir}/overrides/rpm
gpgcheck=0
EOF
        manifest=${tmp_overridesdir}/coreos-assembler-override-manifest.yaml
    fi

    rm -f ${changed_stamp}
    set -x
    sudo rpm-ostree compose tree --repo=${workdir}/repo-build --cachedir=${workdir}/cache \
         --touch-if-changed "${changed_stamp}" \
         ${treecompose_args} \
         ${TREECOMPOSE_FLAGS:-} ${manifest} "$@"
    set +x
}
