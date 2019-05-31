#!/usr/bin/env bash
set -euo pipefail
# Shared shell script library

DIR=$(dirname "$0")
RFC3339="%Y-%m-%dT%H:%M:%SZ"

# Set PYTHONUNBUFFERED=1 so that we get unbuffered output. We should
# be able to do this on the shebang lines but env doesn't support args
# right now. In Fedora we should be able to use the `env -S` option.
export PYTHONUNBUFFERED=1

# Detect what platform we are on
if grep -q '^Fedora' /etc/redhat-release; then
    export ISFEDORA=1
    export ISEL=''
elif grep -q '^Red Hat' /etc/redhat-release; then
    export ISFEDORA=''
    export ISEL=1
else
    echo 1>&2 "should be on either RHEL or Fedora"
    exit 1
fi

info() {
    echo "info: $*" 1>&2
}

fatal() {
    echo "fatal: $*" 1>&2; exit 1
}

# Get target architecture
arch=$(uname -m)
export arch

case $arch in
    "x86_64")  VM_TERMINAL="ttyS0"    ;;
    "ppc64le") VM_TERMINAL="hvc0"     ;;
    "aarch64") VM_TERMINAL="ttyAMA0"  ;;
    "s390x")   VM_TERMINAL="ttysclp0" ;;
    *)         fatal "Architecture $(arch) not supported"
esac
export VM_TERMINAL

if [ -x /usr/libexec/qemu-kvm ]; then
    QEMU_KVM="/usr/libexec/qemu-kvm"
else
    # Enable arch-specific options for qemu
    case "$(arch)" in
        "x86_64")  QEMU_KVM="qemu-system-$(arch) -accel kvm"                          ;;
        "aarch64") QEMU_KVM="qemu-system-$(arch) -accel kvm -M virt,gic-version=host" ;;
        "ppc64le") QEMU_KVM="qemu-system-ppc64 -accel kvm"                            ;;
        "s390x")   QEMU_KVM="qemu-system-$(arch) -accel kvm"                          ;;
        *)         fatal "Architecture $(arch) not supported"
    esac
fi
export QEMU_KVM

_privileged=
has_privileges() {
    if [ -z "${_privileged:-}" ]; then
        if [ -n "${FORCE_UNPRIVILEGED:-}" ]; then
            info "Detected FORCE_UNPRIVILEGED; using virt"
            _privileged=0
        elif ! capsh --print | grep -q 'Bounding.*cap_sys_admin'; then
            info "Missing CAP_SYS_ADMIN; using virt"
            _privileged=0
        elif ! sudo true; then
            info "Missing sudo privs; using virt"
            _privileged=0
        else
            _privileged=1
        fi
    fi
    [ ${_privileged} == 1 ]
}

depcheck() {
    # Verify we have all dependencies installed
    if [ -z "$1" ]; then
      fatal "Must provide a list of RPMs to verify"
    fi

    local deps
    deps=$1
    # Explicitly check the packages in one rpm -q to avoid
    # overhead, only drop down to individual calls if that fails.
    # We use --whatprovides so we handle file paths too.
    #
    # We actually want this var to be split on words
    # shellcheck disable=SC2086
    if ! rpm -q --whatprovides ${deps} &>/dev/null; then
        local missing=""
        for dep in ${deps}; do
            if ! rpm -q --whatprovides ${dep} &>/dev/null; then
                missing="$missing $dep"
            fi
        done
        fatal "Failed to find expected dependencies: $missing"
    fi
}

preflight() {
    # Verify we have all dependencies
    local deps
    [ -n "${ISFEDORA}" ] && filter='^#FEDORA '
    [ -n "${ISEL}" ]     && filter='^#EL7 '
    deps=$(sed "s/${filter}//" /usr/lib/coreos-assembler/deps.txt | grep -v '^#')
    archdeps=$(sed "s/${filter}//" /usr/lib/coreos-assembler/deps-"$(arch)".txt | grep -v '^#')

    depcheck "${deps} ${archdeps}"

    if [ "$(stat -f --printf="%T" .)" = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to find /dev/kvm"
    fi

    # permissions on /dev/kvm vary by (host) distro.  If it's
    # not writable, recreate it.
    if ! [ -w /dev/kvm ]; then
        if ! has_privileges; then
            fatal "running unprivileged, and /dev/kvm not writable"
        else
            sudo rm -f /dev/kvm
            sudo mknod /dev/kvm c 10 232
            sudo setfacl -m u:"$USER":rw /dev/kvm
        fi
    fi

    if ! has_privileges && [ -n "${ISEL}" ]; then
        fatal "running on EL requires privileged mode"
    fi
}

prepare_build() {
    preflight
    if ! [ -d builds ]; then
        fatal "No $(pwd)/builds found; did you run coreos-assembler init?"
    elif ! has_privileges; then
        if [ ! -f cache/cache.qcow2 ]; then
            qemu-img create -f qcow2 cache/cache.qcow2 10G
            LIBGUESTFS_BACKEND=direct virt-format --filesystem=xfs -a cache/cache.qcow2
        fi
    fi

    workdir="$(pwd)"
    configdir=${workdir}/src/config
    manifest=${configdir}/manifest.yaml
    export workdir configdir manifest

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    echo "Using manifest: ${manifest}"

    tmprepo=${workdir}/tmp/repo
    if [ ! -d "${tmprepo}" ]; then
        # backcompat: just move the toplevel repo/
        if [ -d "${workdir}/repo" ]; then
            mv -T "${workdir}/repo" "${tmprepo}"
            rm -f "${tmprepo}/summary"
        else
            ostree init --repo="${tmprepo}" --mode=archive
        fi
    fi

    configdir_gitrepo=${configdir}
    if [ -e "${workdir}/src/config-git" ]; then
        configdir_gitrepo="${workdir}/src/config-git"
    fi
    export configdir_gitrepo

    manifest_tmp_json=${workdir}/tmp/manifest.json
    rpm-ostree compose tree --repo="${tmprepo}" --print-only "${manifest}" > "${manifest_tmp_json}"

    # Abuse the rojig/name as the name of the VM images
    # Also grab rojig summary for image upload descriptions
    name=$(jq -r '.rojig.name' < "${manifest_tmp_json}")
    summary=$(jq -r '.rojig.summary' < "${manifest_tmp_json}")
    license=$(jq -r '.rojig.license' < "${manifest_tmp_json}")
    ref=$(jq -r '.ref' < "${manifest_tmp_json}")
    ref_is_temp=""
    if [ "${ref}" = "null" ]; then
        ref="tmpref-${name}"
        ref_is_temp=1
    fi
    export name ref summary license
    rm -f "${manifest_tmp_json}"

    # This dir is no longer used
    rm builds/work -rf

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p "${workdir}"/tmp

    # Needs to be absolute for rpm-ostree today
    changed_stamp=$(pwd)/tmp/treecompose.changed
    export changed_stamp

    # Allocate temporary space for this build
    tmp_builddir=${workdir}/tmp/build
    rm "${tmp_builddir}" -rf
    mkdir "${tmp_builddir}"
    # And everything after this assumes it's in the temp builddir
    # In case `cd` fails:  https://github.com/koalaman/shellcheck/wiki/SC2164
    cd "${tmp_builddir}" || exit
    # *This* tmp directory is truly temporary to this build, and
    # contains artifacts we definitely don't want to outlive it, unlike
    # other things in ${workdir}/tmp. But we don't export it since e.g. if it's
    # over an NFS mount (like a PVC in OCP), some apps might error out.
    mkdir tmp && TMPDIR=$(pwd)/tmp
}

runcompose() {
    # Implement support for automatic local overrides:
    # https://github.com/coreos/coreos-assembler/issues/118
    local overridesdir=${workdir}/overrides
    local tmp_overridesdir=${TMPDIR}/override
    local override_manifest="${tmp_overridesdir}"/coreos-assembler-override-manifest.yaml
    if [ -d "${overridesdir}"/rpm ] || [ -n "${ref_is_temp}" ] || [ -d "${configdir}/overlay" ]; then
        mkdir "${tmp_overridesdir}"
        cat > "${override_manifest}" <<EOF
include: ${workdir}/src/config/manifest.yaml
EOF
        # Because right now rpm-ostree doesn't look for .repo files in
        # each included dir.
        # https://github.com/projectatomic/rpm-ostree/issues/1628
        cp "${workdir}"/src/config/*.repo "${tmp_overridesdir}"/
        manifest=${override_manifest}
    fi
    if [ -n "${ref_is_temp}" ]; then
        echo 'ref: "'"${ref}"'"' >> "${override_manifest}"
    fi
    if [ -d "${configdir}/overlay" ]; then
        cat >> "${override_manifest}" <<EOF
packages:
  - ${name}-overlay
EOF
        mkdir -p "${overridesdir}"/rpm
        mkdir tmp/overlay-build
        (cd tmp/overlay-build && "${DIR}"/build_rpm_from_dir "${configdir}/overlay" "${name}-overlay" "${workdir}/overrides/rpm")
    fi
    if [[ -n $(ls "${overridesdir}/rpm/"*.rpm 2> /dev/null) ]]; then
        (cd "${overridesdir}"/rpm && createrepo_c .)
        echo "Using RPM overrides from: ${overridesdir}/rpm"
        cat >> "${override_manifest}" <<EOF
repos:
  - coreos-assembler-local-overrides
EOF
        cat > "${tmp_overridesdir}"/coreos-assembler-local-overrides.repo <<EOF
[coreos-assembler-local-overrides]
name=coreos-assembler-local-overrides
baseurl=file://${workdir}/overrides/rpm
gpgcheck=0
EOF
    fi

    rm -f "${changed_stamp}"

    # shellcheck disable=SC2086
    set - ${COSA_RPMOSTREE_GDB:-} rpm-ostree compose tree --repo="${tmprepo}" \
            --cachedir="${workdir}"/cache --touch-if-changed "${changed_stamp}" \
            --unified-core --no-parent "${manifest}" ${COSA_RPMOSTREE_ARGS:-} "$@"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        sudo -E "$@"
        sudo chown -R -h "${USER}":"${USER}" "${tmprepo}"
    else
        runvm "$@"
    fi
}

runvm() {
    local vmpreparedir=${workdir}/tmp/supermin.prepare
    local vmbuilddir=${workdir}/tmp/supermin.build

    rm -rf "${vmpreparedir}" "${vmbuilddir}"
    mkdir -p "${vmpreparedir}" "${vmbuilddir}"

    local rpms
    # then add all the base deps
    # for syntax see: https://github.com/koalaman/shellcheck/wiki/SC2031
    [ -n "${ISFEDORA}" ] && filter='^#FEDORA '
    [ -n "${ISEL}" ]     && filter='^#EL7 '
    rpms=$(sed "s/${filter}//" "${DIR}"/vmdeps.txt | grep -v '^#')
    # shellcheck disable=SC2086
    supermin --prepare --use-installed -o "${vmpreparedir}" $rpms

    # include COSA in the image
    find /usr/lib/coreos-assembler/ -type f > "${vmpreparedir}/hostfiles"

    # the reason we do a heredoc here is so that the var substition takes
    # place immediately instead of having to proxy them through to the VM
    cat > "${vmpreparedir}/init" <<EOF
#!/bin/bash
set -xeuo pipefail
export PATH=/usr/sbin:$PATH
workdir=${workdir}

# use the builder user's id, otherwise some operations like
# chmod will set ownership to root, not builder
export USER=$(id -u)

# ensure the user of files created do not have root ownership
trap 'chown -h -R ${USER}:${USER} ${workdir}' EXIT

$(cat "${DIR}"/supermin-init-prelude.sh)
rc=0
sh ${TMPDIR}/cmd.sh || rc=\$?
echo \$rc > ${workdir}/tmp/rc
/sbin/fstrim -v ${workdir}/cache
/sbin/reboot -f
EOF
    chmod a+x "${vmpreparedir}"/init
    (cd "${vmpreparedir}" && tar -czf init.tar.gz --remove-files init)
    supermin --build "${vmpreparedir}" --size 5G -f ext2 -o "${vmbuilddir}"

    echo "$@" > "${TMPDIR}"/cmd.sh

    # support local dev cases where src/config is a symlink
    srcvirtfs=()
    if [ -L "${workdir}/src/config" ]; then
        # qemu follows symlinks
        srcvirtfs=("-virtfs" "local,id=source,path=${workdir}/src/config,security_model=none,mount_tag=source")
    fi

    pcibus=pci.0
    arch_args=
    if [ "$(arch)" = "aarch64" ]; then
        # 'pci' bus doesn't work on aarch64
        pcibus=pcie.0
        arch_args='-bios /usr/share/AAVMF/AAVMF_CODE.fd'
    fi

    #shellcheck disable=SC2086
    ${QEMU_KVM} ${arch_args:-} \
        -nodefaults -nographic -m 2048 -no-reboot -cpu host \
        -kernel "${vmbuilddir}/kernel" \
        -initrd "${vmbuilddir}/initrd" \
        -netdev user,id=eth0,hostname=supermin \
        -device virtio-net-pci,netdev=eth0 \
        -device virtio-scsi-pci,id=scsi0,bus=${pcibus},addr=0x3 \
        -object rng-random,filename=/dev/urandom,id=rng0 -device virtio-rng-pci,rng=rng0 \
        -drive if=none,id=drive-scsi0-0-0-0,snapshot=on,file="${vmbuilddir}/root" \
        -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=0,drive=drive-scsi0-0-0-0,id=scsi0-0-0-0,bootindex=1 \
        -drive if=none,id=drive-scsi0-0-0-1,discard=unmap,file="${workdir}/cache/cache.qcow2" \
        -device scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=1,drive=drive-scsi0-0-0-1,id=scsi0-0-0-1 \
        -virtfs local,id=workdir,path="${workdir}",security_model=none,mount_tag=workdir \
        "${srcvirtfs[@]}" -serial stdio -append "root=/dev/sda console=${VM_TERMINAL} selinux=1 enforcing=0 autorelabel=1"

    if [ ! -f "${workdir}"/tmp/rc ]; then
        fatal "Couldn't find rc file, something went terribly wrong!"
    fi
    return "$(cat "${workdir}"/tmp/rc)"
}

openshift_git_hack() {
    # When OPENSHIFT_GIT_HACK is defined as a build environment variable,
    # this will fetch the missing .git into the directory where its expected.

    # Some versions of Openshift do not include the GIT repo in the checkout.
    # This may be needed for Openshift versions 3.9 and earlier.
    # See https://github.com/coreos/coreos-assembler/issues/320 and
    #      https://github.com/coreos/coreos-assembler/pull/341
    #
    # Due to limitations in RHEL 7, using "GIT_WORK_TREE" options does not work with
    # submodules. As a result, a new checkout is done, which is rsynced into the
    # source; rsync is less than ideal, but it allows a work-around for the combination
    # of RHEL 7 and problematic Openshift versions.

    # This hack SHOULD ONLY BE HIT WHEN RUNNING IN A CI SITUATION, under
    # the following assumptions:
    #  - The source being built originated from a clean git checkout.
    #  - The `.git` repository has been stripped out by CI.
    #  - The envVar parameters of $OPENSHIFT_GIT_HACK, $GIT_REF, and $GIT_URL are
    #    defined, signalling that this code is intended to run. See
    #    https://github.com/coreos/coreos-assembler/pull/324 for an example.

    local gitd=${1}; shift;

    if [ "${OPENSHIFT_GIT_HACK:-x}" == "x" ]; then
        return
    fi

    if [ -d "${gitd}/.git" ]; then
        return
    fi

    info "NOTICE: using workaround for Openshift missing .git"

    if [ "${GIT_URL:-x}" == "x" ] ||  [ "${GIT_REF:-x}" ]; then
        tmpgit="$(mktemp -d)"
        trap 'rm -rf ${tmpgit}; unset tmpgit;' EXIT

        info "Re-creating git tree in ${gitd} from ${GIT_URL}, ref ${GIT_REF}"
        git clone --depth 1 -b "${GIT_REF}" "${GIT_URL}" "${tmpgit}/clone"

        if test -f "${gitd}/.gitmodules"; then
            pushd "${tmpgit}/clone"
            info "Ensuring git modules exists too"
            git submodule update --init --force
            popd
        fi

        # For more context see the discussion in https://github.com/coreos/coreos-assembler/pull/341
        rsync -av --ignore-existing "${tmpgit}/clone/" "${gitd}"/
    fi
}


prepare_git_artifacts() {
    # prepare_git_artifacts prepares two artifacts from a GIT repo:
    #   1. JSON describing the GIT tree.
    #   2. A tarball of the source.
    local gitd="${1:?first argument must be the git directory}"; shift;
    local tarball="${1:?second argument me be the tarball name}"; shift;
    local json="${1:?third argument must be the json file name to emit}"; shift;

    openshift_git_hack "${gitd}"

    local is_dirty="false"
    local head_ref="unknown"
    local head_remote="unknown"
    local head_url="unknown"
    local gc="git --work-tree=${gitd} --git-dir=${gitd}/.git"

    # shellcheck disable=SC2086
    if ! ${gc} diff --quiet --exit-code; then
        is_dirty="true"
    fi

    tar -C "${gitd}" -czf "${tarball}" --exclude-vcs .
    chmod 0444 "${tarball}"

    local rev
    local branch
    # shellcheck disable=SC2086
    rev="$($gc rev-parse HEAD)"
    branch="$($gc rev-parse --abbrev-ref HEAD)"

    if [[ -f "${gitd}"/.git/shallow || "${branch}" == "HEAD" ]]; then
        # When the checkout is shallow or a detached HEAD, assume the origin is the remote
        # shellcheck disable=SC2086
        head_url="$($gc remote get-url origin 2> /dev/null || echo unknown)"
    else
        # Get the ref name, e.g. remote/origin/master
        # shellcheck disable=SC2086
        head_ref="$($gc symbolic-ref -q HEAD)"
        # Find the remote name, e.g. origin.
        # shellcheck disable=SC2086
        head_remote="$($gc for-each-ref --format='%(upstream:remotename)' ${head_ref} 2> /dev/null || echo unknown)"
        # Find the URL for the remote name, eg. https://github.com/coreos/coreos-assembler
        # shellcheck disable=SC2086
        head_url="$($gc remote get-url ${head_remote} 2> /dev/null || echo unknown)" # get the URL for the remote
    fi

    info "Directory ${gitd}, is from branch ${branch}, commit ${rev}"

    # shellcheck disable=SC2046 disable=SC2086
    cat > "${json}" <<EOC
{
    "date": "$(date -u +$RFC3339)",
    "git": {
        "commit": "${rev}",
        "origin": "${head_url}",
        "branch": "${branch}",
        "dirty": "${is_dirty}"
    },
    "file": {
        "checksum": "$(sha256sum ${tarball} | awk '{print$1}')",
        "checksum_type": "sha256",
        "format": "tar.gz",
        "name": "$(basename ${tarball})",
        "size": $(stat --format=%s ${tarball})
    }
}
EOC
    chmod 0444 "${json}"
}

jq_git() {
    # jq_git extracts JSON elements generated using prepare_git_artifacts.
    # ARG1 is the element name, and ARG2 is the location of the
    # json document.
    jq -rM ".git.$1" "${2}"
}

sha256sum_str() {
    sha256sum | cut -f 1 -d ' '
}

# This generates the "base image"; not specific to a platform.
run_virtinstall() {
    local ostree_repo=$1; shift
    local ostree_rev=$1; shift
    local dest=$1; shift
    local tmpdest="${dest}.tmp"

    # forgive me for this sin
    local iso_location
    iso_location=$(find /usr/lib/coreos-assembler-anaconda/ -name '*.iso' | head -1)

    # if the ref is temporary, then we want a checksum refspec
    if [ -n "${ref_is_temp}" ]; then
        set -- "$@" --delete-ostree-ref
        ostree_rev=$(ostree rev-parse --repo="${ostree_repo}" "${ostree_rev}")
    fi

    /usr/lib/coreos-assembler/virt-install --create-disk --dest="${tmpdest}" \
                                           --tmpdir "${PWD}/tmp" --fs9p \
                                           --kickstart-out "${PWD}"/tmp/flattened.ks \
                                           --console-log-file "${PWD}/install.log" \
                                           --ostree-remote="${name}" --ostree-stateroot="${name}" \
                                           --ostree-ref="${ostree_rev}" \
                                           --location "${iso_location}" \
                                           --configdir="${configdir}" \
                                           --ostree-repo="${ostree_repo}" "$@"
    mv "${tmpdest}" "${dest}"
}

get_latest_build() {
    if [ -L builds/latest ]; then
        readlink builds/latest
    fi
}

get_latest_qemu() {
    local latest
    latest=$(get_latest_build)
    if [ -n "$latest" ]; then
        # shellcheck disable=SC2086
        ls builds/${latest}/*-qemu.qcow2
    fi
}
