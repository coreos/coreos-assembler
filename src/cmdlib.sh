#!/usr/bin/env bash
set -euo pipefail
# Shared shell script library

DIR=$(dirname "$(realpath "${BASH_SOURCE[0]}")")
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
    if test -t 1; then
        echo "$(tput setaf 1)fatal:$(tput sgr0) $*" 1>&2
    else
        echo "fatal: $*" 1>&2
    fi
    exit 1
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

case $arch in
    "x86_64")  DEFAULT_TERMINAL="ttyS0"   ;;
    "ppc64le") DEFAULT_TERMINAL="hvc0"    ;;
    "aarch64") DEFAULT_TERMINAL="ttyAMA0" ;;
    "s390x")   DEFAULT_TERMINAL="ttysclp0";;
    *)         fatal "Architecture ${arch} not supported"
esac
export DEFAULT_TERMINAL

COSA_PRIVILEGED=
has_privileges() {
    if [ -z "${COSA_PRIVILEGED:-}" ]; then
        if [ -n "${FORCE_UNPRIVILEGED:-}" ]; then
            info "Detected FORCE_UNPRIVILEGED; using virt"
            COSA_PRIVILEGED=0
        elif ! capsh --print | grep -q 'Bounding.*cap_sys_admin'; then
            info "Missing CAP_SYS_ADMIN; using virt"
            COSA_PRIVILEGED=0
        elif [ "$(id -u)" != "0" ] && ! sudo true; then
            info "Missing sudo privs; using virt"
            COSA_PRIVILEGED=0
        else
            COSA_PRIVILEGED=1
        fi
        export COSA_PRIVILEGED
    fi
    [ ${COSA_PRIVILEGED} == 1 ]
}

depcheck() {
    # Allow suppressing this so we can e.g. override
    # rpm-ostree in CI flows without building an RPM.
    if test -n "${COSA_SUPPRESS_DEPCHECK:-}"; then
        return
    fi
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

    if [ "$(stat -f --printf="%T" .)" = "overlayfs" ] && [ -z "${COSA_SKIP_OVERLAY:-}" ]; then
        fatal "$(pwd) must be a volume"
    fi

    # See https://pagure.io/centos-infra/issue/48
    if test "$(umask)" = 0000; then
        fatal "Your umask is unset, please use umask 0022 or so"
    fi
}

preflight_kvm() {
    # permissions on /dev/kvm vary by (host) distro.  If it's
    # not writable, recreate it.

    if test -z "${COSA_NO_KVM:-}"; then
        if ! test -c /dev/kvm; then
            fatal "Missing /dev/kvm"
        fi
        if ! [ -w /dev/kvm ]; then
            if ! has_privileges; then
                fatal "running unprivileged, and /dev/kvm not writable"
            else
                sudo rm -f /dev/kvm
                sudo mknod /dev/kvm c 10 232
                sudo setfacl -m u:"$USER":rw /dev/kvm
            fi
        fi
    fi
}

# Picks between yaml or json based on which version exists. Errors out if both
# exists. If neither exist, prefers the extension in ${2}, or otherwise YAML.
pick_yaml_or_else_json() {
    local f=$1; shift
    local default=${1:-yaml}; shift
    if [ -f "${f}.json" ] && [ -f "${f}.yaml" ]; then
        fatal "Found both ${f}.json and ${f}.yaml"
    elif [ -f "${f}.json" ]; then
        echo "${f}.json"
    elif [ -f "${f}.yaml" ]; then
        echo "${f}.yaml"
    else
        echo "${f}.${default}"
    fi
}

# Given a YAML file at first path, write it as JSON to file at second path
yaml2json() {
    python3 -c 'import sys, json, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)' < "$1" > "$2"
}

prepare_build() {
    preflight
    preflight_kvm
    if ! [ -d builds ]; then
        fatal "No $(pwd)/builds found; did you run coreos-assembler init?"
    fi

    workdir="$(pwd)"

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p "${workdir}"/tmp

    # Allocate temporary space for this build
    export tmp_builddir="${workdir}/tmp/build${IMAGE_TYPE:+.$IMAGE_TYPE}"
    rm "${tmp_builddir}" -rf
    mkdir "${tmp_builddir}"

    configdir=${workdir}/src/config
    manifest=${configdir}/manifest.yaml
    # for the base lockfile, we default to JSON since that's what rpm-ostree
    # actually outputs
    manifest_lock=$(pick_yaml_or_else_json "${configdir}/manifest-lock.${basearch}" json)
    manifest_lock_overrides=$(pick_yaml_or_else_json "${configdir}/manifest-lock.overrides")
    manifest_lock_arch_overrides=$(pick_yaml_or_else_json "${configdir}/manifest-lock.overrides.${basearch}")
    fetch_stamp="${workdir}"/cache/fetched-stamp

    image_yaml="${workdir}/tmp/image.yaml"
    flatten_image_yaml_to_file "${configdir}/image.yaml" "${image_yaml}"
    # Convert the image.yaml to JSON so that it can be more easily parsed
    # by the shell script in create_disk.sh.
    image_json="${workdir}/tmp/image.json"
    yaml2json "${image_yaml}" "${image_json}"

    export workdir configdir manifest manifest_lock manifest_lock_overrides manifest_lock_arch_overrides
    export fetch_stamp image_json

    if ! [ -f "${manifest}" ]; then
        fatal "Failed to find ${manifest}"
    fi

    if [ -d "${configdir}/.git" ]; then
        (cd "${configdir}" && echo -n "Config commit: " && git describe --tags --always --abbrev=42)
    fi
    echo "Using manifest: ${manifest}"

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

    manifest_tmp_json=${tmp_builddir}/manifest.json
    rpm-ostree compose tree --repo="${tmprepo}" --print-only "${manifest}" > "${manifest_tmp_json}"

    # Abuse the rojig/name as the name of the VM images
    # Also grab rojig summary for image upload descriptions
    name=$(jq -r '.rojig.name' < "${manifest_tmp_json}")
    summary=$(jq -r '.rojig.summary' < "${manifest_tmp_json}")
    ref=$(jq -r '.ref//""' < "${manifest_tmp_json}")
    export name ref summary
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

    # And everything after this assumes it's in the temp builddir
    # In case `cd` fails:  https://github.com/koalaman/shellcheck/wiki/SC2164
    cd "${tmp_builddir}" || exit
    # *This* tmp directory is truly temporary to this build, and
    # contains artifacts we definitely don't want to outlive it, unlike
    # other things in ${workdir}/tmp. But we don't export it since e.g. if it's
    # over an NFS mount (like a PVC in OCP), some apps might error out.
    mkdir -p tmp && export TMPDIR="${workdir}/tmp"

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
    # Only keep write bit for owner for all overlay files/dirs. We copy it over
    # but with the umask we want so the perms are dropped. This is easier than
    # using ostree's --statoverride when dealing with executable files. See:
    # https://github.com/ostreedev/ostree/issues/2368
    rm -rf "${TMPDIR}/overlay" && (umask 0022 && cp -r "${respath}" "${TMPDIR}/overlay")
    # Make sure there are no setgid/setuid bits in the overlays.
    # See e.g. https://github.com/coreos/fedora-coreos-tracker/issues/1003.
    chmod -R gu-s "${TMPDIR}/overlay"
    # Apply statoverrides from a file in the root of the overlay, which may
    # or may not exist.  ostree doesn't support comments in statoverride
    # files, but we do.
    touch "${TMPDIR}/overlay/statoverride"
    echo -n "Committing ${name}: ${path} ... "
    ostree commit --repo="${tmprepo}" --tree=dir="${TMPDIR}/overlay" -b "${name}" \
        --owner-uid 0 --owner-gid 0 --no-xattrs --no-bindings --parent=none \
        --mode-ro-executables --timestamp "${git_timestamp}" \
        --statoverride <(sed -e '/^#/d' "${TMPDIR}/overlay/statoverride") \
        --skip-list <(echo /statoverride)
}

# Implement support for automatic local overrides:
# https://github.com/coreos/coreos-assembler/issues/118
#
# This function commits the contents of overlay.d/ as well
# as overrides/{rootfs} to OSTree commits, and also handles
# overrides/rpm.
prepare_compose_overlays() {
    local with_cosa_overrides=1
    while [ $# -gt 0 ]; do
        flag="${1}"; shift;
        case "${flag}" in
            --ignore-cosa-overrides) with_cosa_overrides=;;
             *) echo "${flag} is not understood."; exit 1;;
         esac;
    done

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

    if [ -d "${overridesdir}" ] || [ -d "${ovld}" ]; then
        mkdir -p "${tmp_overridesdir}"
        cat > "${override_manifest}" <<EOF
include: ${manifest}
EOF
        # Because right now rpm-ostree doesn't look for .repo files in
        # each included dir.
        # https://github.com/projectatomic/rpm-ostree/issues/1628
        cp "${workdir}"/src/config/*.repo "${tmp_overridesdir}"/
        manifest=${override_manifest}
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

    local_overrides_lockfile="${tmp_overridesdir}/local-overrides.json"
    if [ -n "${with_cosa_overrides}" ] && [[ -n $(ls "${overridesdir}/rpm/"*.rpm 2> /dev/null) ]]; then
        (cd "${overridesdir}"/rpm && rm -rf .repodata && createrepo_c .)
        # synthesize an override lockfile to force rpm-ostree to pick up our
        # override RPMS -- we try to be nice here and allow multiple versions of
        # the same RPMs: the `dnf repoquery` below is to pick the latest one
        dnf repoquery  --repofrompath=tmp,"file://${overridesdir}/rpm" \
            --disablerepo '*' --enablerepo tmp --refresh --latest-limit 1 \
            --exclude '*.src' --qf '%{NAME}\t%{EVR}\t%{ARCH}' --quiet | python3 -c '
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
cost=500
EOF
    else
        rm -vf "${local_overrides_lockfile}"
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
runcompose_tree() {
    local tmp_overridesdir=${TMPDIR}/override
    if [ -f "${tmp_overridesdir}/local-overrides.json" ]; then
        # we need our overrides to be at the end of the list
        set - "$@" --ex-lockfile="${tmp_overridesdir}/local-overrides.json"
    fi
    impl_rpmostree_compose tree --unified-core "${manifest}" "$@"
    if has_privileges; then
        sudo chown -R -h "${USER}":"${USER}" "${tmprepo}"
    fi
}

runcompose_extensions() {
    local outputdir=$1; shift
    impl_rpmostree_compose extensions "$@" --output-dir "$outputdir"
    if has_privileges; then
        sudo chown -R -h "${USER}":"${USER}" "${outputdir}"
    fi
}

impl_rpmostree_compose() {
    local cmd=$1; shift
    local workdir=${workdir:-$(pwd)}
    local repo=${tmprepo:-${workdir}/tmp/repo}

    rm -f "${changed_stamp}"
    # shellcheck disable=SC2086
    set - ${COSA_RPMOSTREE_GDB:-} rpm-ostree compose "${cmd}" --repo="${repo}" \
            --touch-if-changed "${changed_stamp}" --cachedir="${workdir}"/cache \
            ${COSA_RPMOSTREE_ARGS:-} "$@"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        # we hardcode a umask of 0022 here to make sure that composes are run
        # with a consistent value, regardless of the environment
        (umask 0022 && sudo -E "$@")
    else
        # "cache2" has an explicit label so we can find it in qemu easily
        if [ ! -f "${workdir}"/cache/cache2.qcow2 ]; then
            qemu-img create -f qcow2 cache2.qcow2.tmp 10G
            (
             # shellcheck source=src/libguestfish.sh
             source /usr/lib/coreos-assembler/libguestfish.sh
             virt-format --filesystem=xfs --label=cosa-cache -a cache2.qcow2.tmp)
            mv -T cache2.qcow2.tmp "${workdir}"/cache/cache2.qcow2
        fi
        # And remove the old one
        rm -vf "${workdir}"/cache/cache.qcow2
        compose_qemu_args+=("-drive" "if=none,id=cache,discard=unmap,file=${workdir}/cache/cache2.qcow2" \
                            "-device" "virtio-blk,drive=cache")
        runvm "${compose_qemu_args[@]}" -- "$@"
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

meta_key() {
    cosa meta --workdir="${workdir}" --build="${build:-latest}" --get-value "${@}"
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

    # tmp_builddir is set in prepare_build, but some stages may not
    # know that it exists.
    # shellcheck disable=SC2086
    export tmp_builddir="${tmp_builddir:-$(mktemp -p ${workdir}/tmp -d supermin.XXXX)}"

    # shellcheck disable=SC2155
    local vmpreparedir="${tmp_builddir}/supermin.prepare"
    local vmbuilddir="${tmp_builddir}/supermin.build"
    local runvm_console="${tmp_builddir}/runvm-console.txt"
    local rc_file="${tmp_builddir}/rc"

    mkdir -p "${vmpreparedir}" "${vmbuilddir}"

    local rpms
    # then add all the base deps
    # for syntax see: https://github.com/koalaman/shellcheck/wiki/SC2031
    rpms=$(grep -v '^#' < "${DIR}"/vmdeps.txt)
    # There seems to be some false positives in shellcheck
    # https://github.com/koalaman/shellcheck/issues/2217
    # shellcheck disable=2031
    archrpms=$(grep -v '^#' < "${DIR}/vmdeps-${arch}.txt")

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
if [ -z "${RUNVM_SHELL:-}" ]; then
  bash ${tmp_builddir}/cmd.sh |& tee /dev/virtio-ports/cosa-cmdout || rc=\$?
else
  bash; poweroff -f -f; sleep infinity
fi
echo \$rc > ${rc_file}
if [ -n "\${cachedev}" ]; then
    /sbin/fstrim -v ${workdir}/cache
fi
/sbin/reboot -f
EOF
    chmod a+x "${vmpreparedir}"/init
    (cd "${vmpreparedir}" && tar -czf init.tar.gz --remove-files init)
    # put the supermin output in a separate file since it's noisy
    if ! supermin --build "${vmpreparedir}" --size 5G -f ext2 -o "${vmbuilddir}" \
            &> "${tmp_builddir}/supermin.out"; then
        cat "${tmp_builddir}/supermin.out"
        fatal "Failed to run: supermin --build"
    fi

    # this is the command run in the supermin container
    # we hardcode a umask of 0022 here to make sure that composes are run
    # with a consistent value, regardless of the environment
    echo "umask 0022" > "${tmp_builddir}"/cmd.sh
    echo "$@" >> "${tmp_builddir}"/cmd.sh

    touch "${runvm_console}"

    # There seems to be some false positives in shellcheck
    # https://github.com/koalaman/shellcheck/issues/2217
    memory_default=2048
    # shellcheck disable=2031
    case $arch in
    # Power 8 page faults with 2G of memory in rpm-ostree
    # Most probably due to radix and 64k overhead.
    "ppc64le") memory_default=4096 ;;
    esac

    kola_args=(kola qemuexec -m "${COSA_SUPERMIN_MEMORY:-${memory_default}}" --auto-cpus -U --workdir none \
               --console-to-file "${runvm_console}")

    base_qemu_args=(-drive 'if=none,id=root,format=raw,snapshot=on,file='"${vmbuilddir}"'/root,index=1' \
                    -device 'virtio-blk,drive=root'
                    -kernel "${vmbuilddir}/kernel" -initrd "${vmbuilddir}/initrd" \
                    -no-reboot -nodefaults \
                    -device virtio-serial \
                    -virtfs 'local,id=workdir,path='"${workdir}"',security_model=none,mount_tag=workdir' \
                    -append "root=/dev/vda console=${DEFAULT_TERMINAL} selinux=1 enforcing=0 autorelabel=1" \
                   )

    # support local dev cases where src/config is a symlink
    if [ -L "${workdir}/src/config" ]; then
        # qemu follows symlinks
        base_qemu_args+=("-virtfs" 'local,id=source,path='"${workdir}"'/src/config,security_model=none,mount_tag=source')
    fi

    if [ -z "${RUNVM_SHELL:-}" ]; then
        if ! "${kola_args[@]}" -- "${base_qemu_args[@]}" \
            -device virtserialport,chardev=virtioserial0,name=cosa-cmdout \
            -chardev stdio,id=virtioserial0 \
            "${qemu_args[@]}" <&-; then # the <&- here closes stdin otherwise qemu waits forever
                cat "${runvm_console}"
                fatal "Failed to run 'kola qemuexec'"
        fi
    else
        exec "${kola_args[@]}" -- "${base_qemu_args[@]}" -serial stdio "${qemu_args[@]}"
    fi

    rm -rf "${tmp_builddir}/supermin.out" "${vmpreparedir}" "${vmbuilddir}"

    if [ ! -f "${rc_file}" ]; then
        cat "${runvm_console}"
        if test -n "${ARTIFACT_DIR:-}"; then
            cp "${runvm_console}" "${ARTIFACT_DIR}"
        fi
        fatal "Couldn't find rc file; failure inside supermin init?"
    fi
    rc="$(cat "${rc_file}")"
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
        # Get the ref name, e.g. remote/origin/main
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

    local checksum name size
    checksum=$(sha256sum "${tarball}" | awk '{print$1}')
    name=$(basename "${tarball}")
    size=$(find "${tarball}" -printf %s)
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
        "checksum": "${checksum}",
        "checksum_type": "sha256",
        "format": "tar.gz",
        "name": "${name}",
        "size": "${size}"
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
    local parent_build=$1; shift
    local dir=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
print(Builds('${workdir:-$(pwd)}').init_build_meta_json('${ostree_commit}', '${parent_build}', '${dir}'))")
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
    local workdir=$1; shift
    local arch=${1:-}
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
builds = Builds('${workdir:-$(pwd)}')
builds.insert_build('${buildid}', basearch='${arch:-}')
builds.bump_timestamp()
print('Build ${buildid} was inserted ${arch:+for $arch}')")
}

flatten_image_yaml_to_file() {
    local srcfile=$1; shift
    local outfile=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib import cmdlib
cmdlib.flatten_image_yaml_to_file('${srcfile}', '${outfile}')")
}

# Shell wrapper for the Python import_ostree_commit
import_ostree_commit_for_build() {
    local buildid=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib import cmdlib
from cosalib.builds import Builds
builds = Builds('${workdir:-$(pwd)}')
builddir = builds.get_build_dir('${buildid}')
buildmeta = builds.get_build_meta('${buildid}')
cmdlib.import_ostree_commit('${workdir:-$(pwd)}/tmp/repo', builddir, buildmeta)
")
}
