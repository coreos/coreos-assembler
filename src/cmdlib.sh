#!/usr/bin/env bash
set -euo pipefail
# Shared shell script library

DIR=$(dirname "$0")
RFC3339="%Y-%m-%dT%H:%M:%SZ"

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

# Get target base architecture
basearch=$(python3 -c '
import gi
gi.require_version("RpmOstree", "1.0")
from gi.repository import RpmOstree
print(RpmOstree.get_basearch())')
export basearch

# Get target architecture
arch=$(uname -m)
export arch
devtype=pci

case $arch in
    "x86_64")  DEFAULT_TERMINAL="ttyS0"    ;;
    "ppc64le") DEFAULT_TERMINAL="hvc0"     ;;
    "aarch64") DEFAULT_TERMINAL="ttyAMA0"  ;;
    "s390x")   DEFAULT_TERMINAL="ttysclp0"; devtype=ccw ;;
    *)         fatal "Architecture $(arch) not supported"
esac
export DEFAULT_TERMINAL devtype

_privileged=
has_privileges() {
    if [ -z "${_privileged:-}" ]; then
        if [ -n "${FORCE_UNPRIVILEGED:-}" ]; then
            info "Detected FORCE_UNPRIVILEGED; using virt"
            _privileged=0
        elif ! capsh --print | grep -q 'Bounding.*cap_sys_admin'; then
            info "Missing CAP_SYS_ADMIN; using virt"
            _privileged=0
        elif [ "$(id -u)" != "0" ] && ! sudo true; then
            info "Missing sudo privs; using virt"
            _privileged=0
        else
            _privileged=1
        fi
    fi
    [ ${_privileged} == 1 ]
}

depcheck() {
    local deps
    deps=$(/usr/lib/coreos-assembler/print-dependencies.sh)
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
    depcheck

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


disk_ignition_version() {
    local path
    path=$1
    # yup, this is happening *again* (see below too)
    python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.cmdlib import disk_ignition_version
print(disk_ignition_version('${path}}'))"
}

# Picks between yaml or json based on which version exists. If neither exist,
# prefers YAML.
pick_yaml_or_else_json() {
    local f=$1; shift
    if [ -f "${f}.json" ] && [ -f "${f}.yaml" ]; then
        fatal "Found both ${f}.json and ${f}.yaml"
    elif [ -f "${f}.json" ]; then
        echo "${f}.json"
    else
        # prefer yaml
        echo "${f}.yaml"
    fi
}

prepare_build() {
    preflight
    if ! [ -d builds ]; then
        fatal "No $(pwd)/builds found; did you run coreos-assembler init?"
    elif ! has_privileges; then
        # "cache2" has an explicit label so we can find it in qemu easily
        if [ ! -f cache/cache2.qcow2 ]; then
            qemu-img create -f qcow2 cache/cache2.qcow2 10G
            LIBGUESTFS_BACKEND=direct virt-format --filesystem=xfs --label=cosa-cache -a cache/cache2.qcow2
        fi
        # And remove the old one
        rm -vf cache/cache.qcow2
    fi

    workdir="$(pwd)"
    configdir=${workdir}/src/config
    manifest=${configdir}/manifest.yaml
    manifest_lock=$(pick_yaml_or_else_json "${configdir}/manifest-lock.${basearch}")
    manifest_lock_overrides=$(pick_yaml_or_else_json "${configdir}/manifest-lock.overrides.${basearch}")
    fetch_stamp="${workdir}"/cache/fetched-stamp

    export workdir configdir manifest manifest_lock manifest_lock_overrides
    export fetch_stamp

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    if [ -d "${configdir}/.git" ]; then
        (cd "${configdir}" && echo -n "Config commit: " && git describe --tags --always --abbrev=42)
    fi
    echo "Using manifest: ${manifest}"

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p "${workdir}"/tmp

    # backcompat for local setups that initialized with `ln -sr`
    if [ -L "${configdir}" ]; then
        if [[ $(readlink "${configdir}") != /* ]]; then
            ln -sfn "$(realpath "${configdir}")" "${configdir}"
        fi
    fi

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
    # And validate fields coreos-assembler requires, but not rpm-ostree
    required_fields=("automatic-version-prefix")
    for field in "${required_fields[@]}"; do
        if ! jq -re '."'"${field}"'"' < "${manifest_tmp_json}" >/dev/null; then
            echo "Missing required field in src/config/manifest.yaml: ${field}" 1>&2
            exit 1
        fi
    done
    rm -f "${manifest_tmp_json}"

    # This dir is no longer used
    rm builds/work -rf

    # Place for cmd-build-fast
    mkdir -p tmp/fastbuilds
    fastbuilddir=$(pwd)/tmp/fastbuild
    export fastbuilddir

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

    # Needs to be absolute for rpm-ostree today
    changed_stamp=${TMPDIR}/treecompose.changed
    export changed_stamp
    overrides_active_stamp=${TMPDIR}/overrides.stamp
    export overrides_active_stamp
}

commit_overlay() {
    local name path respath
    name=$1
    path=$2
    respath=$(realpath "${path}")
    echo -n "Committing ${name}: ${path} ... "
    ostree commit --repo="${tmprepo}" --tree=dir="${respath}" -b "${name}" \
        --owner-uid 0 --owner-gid 0 --no-xattrs --no-bindings --parent=none \
        --timestamp "${git_timestamp}"
}

# Implement support for automatic local overrides:
# https://github.com/coreos/coreos-assembler/issues/118
#
# This function commits the contents of overlay.d/ as well
# as overrides/{rootfs} to OSTree commits, and also handles
# overrides/rpm.
prepare_compose_overlays() {
    local overridesdir=${workdir}/overrides
    local tmp_overridesdir=${TMPDIR}/override
    local override_manifest="${tmp_overridesdir}"/coreos-assembler-override-manifest.yaml
    local ovld="${configdir}/overlay.d"
    local git_timestamp="January 1 1970"
    local layers=""
    if [ -d "${configdir_gitrepo}/.git" ]; then
        git_timestamp=$(git -C "${configdir_gitrepo}" show -s --format=%ci HEAD)
    fi

    if [ -d "${configdir}/overlay" ]; then
        (echo "ERROR: overlay/ directory is no longer supported, use overlay.d/"
         echo "ERROR: https://github.com/coreos/coreos-assembler/pull/639") 1>&2
        exit 1
    fi

    if [ -d "${overridesdir}" ] || [ -n "${ref_is_temp}" ] || [ -d "${ovld}" ]; then
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


    if [ -d "${ovld}" ]; then
        for n in "${ovld}"/*; do
            if ! [ -d "${n}" ]; then
                continue
            fi
            local bn ovlname
            bn=$(basename "${n}")
            ovlname="${name}-config-overlay-${bn}"
            commit_overlay "${ovlname}" "${n}"
            layers="${layers} ${ovlname}"
        done
    fi

    if [ -n "${layers}" ]; then
        echo "ostree-layers:" >> "${override_manifest}"
        for layer in ${layers}; do
            echo "  - ${layer}" >> "${override_manifest}"
        done
    fi
    if [[ -n $(ls "${overridesdir}/rpm/"*.rpm 2> /dev/null) ]]; then
        (cd "${overridesdir}"/rpm && rm -rf .repodata && createrepo_c .)
        # synthesize an override lockfile to force rpm-ostree to pick up our
        # override RPMS -- we try to be nice here and allow multiple versions of
        # the same RPMs: the `dnf repoquery` below is to pick the latest one
        local_overrides_lockfile="${tmp_overridesdir}/local-overrides.json"
        dnf repoquery  --repofrompath=tmp,"file://${overridesdir}/rpm" \
            --disablerepo '*' --enablerepo tmp --refresh --latest-limit 1 \
            --qf '%{NAME}\t%{EVR}\t%{ARCH}' --quiet | python3 -c '
import sys, json
lockfile = {"packages": {}}
for line in sys.stdin:
    name, evr, arch = line.strip().split("\t")
    lockfile["packages"][name] = {"evra": f"{evr}.{arch}"}
json.dump(lockfile, sys.stdout)' > "${local_overrides_lockfile}"

        echo "Using RPM overrides from: ${overridesdir}/rpm"
        touch "${overrides_active_stamp}"
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
    rootfs_overrides="${overridesdir}/rootfs"
    if [[ -d "${rootfs_overrides}" && -n $(ls -A "${rootfs_overrides}") ]]; then
        echo -n "Committing ${rootfs_overrides}... "
        touch "${overrides_active_stamp}"
        ostree commit --repo="${tmprepo}" --tree=dir="${rootfs_overrides}" -b cosa-overrides-rootfs \
          --owner-uid 0 --owner-gid 0 --no-xattrs --no-bindings --parent=none \
          --timestamp "${git_timestamp}"
          cat >> "${override_manifest}" << EOF
ostree-override-layers:
  - cosa-overrides-rootfs
EOF
    fi
}

# Wrapper for `rpm-ostree compose tree` which adds some default options
# such as `--repo` (which is auto-derived from the builddir) and
# `--unified-core` that we always want.  Also dispatches to supermin if
# we're running without support for nested containerization.
runcompose() {
    local tmp_overridesdir=${TMPDIR}/override

    if [ -f "${tmp_overridesdir}/local-overrides.json" ]; then
        # we need our overrides to be at the end of the list
        set - "$@" --ex-lockfile="${tmp_overridesdir}/local-overrides.json"
    fi

    # shellcheck disable=SC2086
    set - ${COSA_RPMOSTREE_GDB:-} rpm-ostree compose tree --repo="${tmprepo}" \
            --cachedir="${workdir}"/cache --touch-if-changed "${changed_stamp}" \
            --unified-core "${manifest}" ${COSA_RPMOSTREE_ARGS:-} "$@"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        sudo -E "$@"
        sudo chown -R -h "${USER}":"${USER}" "${tmprepo}"
    else
        runvm -- "$@"
    fi
}

# Strips out the digest field from lockfiles since they subtly conflict with
# various workflows.
strip_out_lockfile_digests() {
    jq 'del(.packages[].digest)' "$1" > "$1.tmp"
    mv "$1.tmp" "$1"
}

json_key() {
    jq -r ".[\"$1\"]" < "${builddir}/meta.json"
}

# runvm generates and runs a minimal VM which we use to "bootstrap" our build
# process.  It mounts the workdir via 9p.  If you need to add new packages into
# the vm, update `vmdeps.txt`.
# If you need to debug it, one trick is to change the `-serial file` below
# into `-serial stdio`, drop the <&- and virtio-serial stuff and then e.g. add
# `bash` into the init process.
runvm() {
    local qemu_args=()
    while true; do
        case "$1" in
            --)
                shift
                break
                ;;
            *)
                qemu_args+=("$1")
                shift
                ;;
        esac
    done
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
    archrpms=$(sed "s/${filter}//" "${DIR}"/vmdeps-"$(arch)".txt | grep -v '^#')
    # shellcheck disable=SC2086
    supermin --prepare --use-installed -o "${vmpreparedir}" $rpms $archrpms

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
export RUNVM_NONET=${RUNVM_NONET:-}
$(cat "${DIR}"/supermin-init-prelude.sh)
rc=0
# tee to the virtio port so its output is also part of the supermin output in
# case e.g. a key msg happens in dmesg when the command does a specific operation
bash ${TMPDIR}/cmd.sh |& tee /dev/virtio-ports/cosa-cmdout || rc=\$?
echo \$rc > ${workdir}/tmp/rc
if [ -n "\${cachedev}" ]; then
    /sbin/fstrim -v ${workdir}/cache
fi
/sbin/reboot -f
EOF
    chmod a+x "${vmpreparedir}"/init
    (cd "${vmpreparedir}" && tar -czf init.tar.gz --remove-files init)
    # put the supermin output in a separate file since it's noisy
    if ! supermin --build "${vmpreparedir}" --size 5G -f ext2 -o "${vmbuilddir}" \
            &> "${workdir}/tmp/supermin.out"; then
        cat "${workdir}/tmp/supermin.out"
        fatal "Failed to run: supermin --build"
    fi
    rm "${workdir}/tmp/supermin.out"

    echo "$@" > "${TMPDIR}"/cmd.sh

    cachedisk=()
    if [ -f "${workdir}/cache/cache2.qcow2" ]; then
        cachedisk=("-drive" "if=none,id=cache,discard=unmap,file=${workdir}/cache/cache2.qcow2" \
                    "-device" "virtio-blk,drive=cache")
    fi

    # support local dev cases where src/config is a symlink
    srcvirtfs=()
    if [ -L "${workdir}/src/config" ]; then
        # qemu follows symlinks
        srcvirtfs=("-virtfs" "local,id=source,path=${workdir}/src/config,security_model=none,mount_tag=source")
    fi

    local runvm_console
    runvm_console="${workdir}/tmp/runvm-console.txt"
    rm -f "${workdir}/tmp/rc" "${runvm_console}"

    touch "${runvm_console}"
    #shellcheck disable=SC2086
    if ! kola qemuexec -m 2048 --auto-cpus -U --workdir none -- -no-reboot -nodefaults -serial file:"${runvm_console}" \
        -kernel "${vmbuilddir}/kernel" -initrd "${vmbuilddir}/initrd" \
        -device virtio-serial \
        -device virtserialport,chardev=virtioserial0,name=cosa-cmdout \
        -chardev stdio,id=virtioserial0 \
        -drive "if=none,id=root,format=raw,snapshot=on,file=${vmbuilddir}/root,index=1" \
        -device "virtio-blk,drive=root" \
        "${cachedisk[@]}" \
        -virtfs local,id=workdir,path="${workdir}",security_model=none,mount_tag=workdir \
        "${srcvirtfs[@]}" -append "root=/dev/vda console=${DEFAULT_TERMINAL} selinux=1 enforcing=0 autorelabel=1" \
        "${qemu_args[@]}" <&-; then # the <&- here closes stdin otherwise qemu waits forever
            cat "${runvm_console}"
            fatal "Failed to run 'kola qemuexec'"
    fi

    if [ ! -f "${workdir}"/tmp/rc ]; then
        cat "${runvm_console}"
        fatal "Couldn't find rc file; failure inside supermin init?"
    fi
    rc="$(cat "${workdir}"/tmp/rc)"
    rm -f "${workdir}/tmp/rc"
    return "${rc}"
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

get_latest_build() {
    if [ -L "${workdir:-$(pwd)}/builds/latest" ]; then
        readlink "${workdir:-$(pwd)}/builds/latest"
    fi
}

get_build_dir() {
    local buildid=$1; shift
    # yup, this is happening
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
print(Builds('${workdir:-$(pwd)}').get_build_dir('${buildid}'))")
}

init_build_meta_json() {
    local ostree_commit=$1; shift
    local dir=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
print(Builds('${workdir:-$(pwd)}').init_build_meta_json('${ostree_commit}', '${dir}'))")
}

get_latest_qemu() {
    local latest builddir
    latest=$(get_latest_build)
    builddir=$(get_build_dir "$latest")
    if [ -n "$latest" ]; then
        # shellcheck disable=SC2086
        ls ${builddir}/*-qemu.qcow2*
    fi
}

insert_build() {
    local buildid=$1; shift
    local dir=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
builds = Builds('${workdir:-$(pwd)}')
builds.insert_build('${buildid}')
builds.bump_timestamp()
print('Build ${buildid} was inserted')")
}
