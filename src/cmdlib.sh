#!/usr/bin/env bash
set -euo pipefail
# Shared shell script library

DIR=$(dirname "$(realpath "${BASH_SOURCE[0]}")")
RFC3339="%Y-%m-%dT%H:%M:%SZ"

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
    "x86_64")  DEFAULT_TERMINAL="ttyS0,115200n8" ;;
    "ppc64le") DEFAULT_TERMINAL="hvc0"           ;;
    "aarch64") DEFAULT_TERMINAL="ttyAMA0"        ;;
    "s390x")   DEFAULT_TERMINAL="ttysclp0"       ;;
    # minimal support; the rest of cosa isn't yet riscv64-aware
    "riscv64") DEFAULT_TERMINAL="ttyS0"          ;;
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
            fatal "Missing /dev/kvm; you can set COSA_NO_KVM=1 to bypass this at the cost of performance."
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

preflight_checks() {
    preflight
    preflight_kvm
}

# Given a YAML file at first path, write it as JSON to file at second path
yaml2json() {
    python3 -c 'import sys, json, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout, sort_keys=True)' < "$1" > "$2"
}


# Run with cache disk.
runvm_with_cache() {
    local cache_size=${RUNVM_CACHE_SIZE:-50G}
    # "cache2" has an explicit label so we can find it in qemu easily
    if [ ! -f "${workdir}"/cache/cache2.qcow2 ]; then
        qemu-img create -f qcow2 cache2.qcow2.tmp "$cache_size"
        (
         # shellcheck source=src/libguestfish.sh
         source /usr/lib/coreos-assembler/libguestfish.sh
         virt-format --filesystem=xfs --label=cosa-cache -a cache2.qcow2.tmp)
        mv -T cache2.qcow2.tmp "${workdir}"/cache/cache2.qcow2
    fi
    # And remove the old one
    rm -vf "${workdir}"/cache/cache.qcow2
    cache_args+=("-drive" "if=none,id=cache,discard=unmap,file=${workdir}/cache/cache2.qcow2" \
                        "-device" "virtio-blk,drive=cache")
    runvm "${cache_args[@]}" "$@"
}

# Strip out the arch field from lockfiles to make them archless.
strip_out_lockfile_arches() {
    python3 -c '
import sys, json
lockfile = json.load(sys.stdin)
new_packages = {}
for pkg, meta in lockfile["packages"].items():
    evra = meta.get("evra")
    if evra is None or evra.endswith(".noarch"):
        new_packages[pkg] = meta
        continue
    new_packages[pkg] = {
        "evr": evra[:evra.rindex(".")]
    }
lockfile["packages"] = new_packages
json.dump(lockfile, sys.stdout)
' < "$1" > "$1.tmp"
    mv "$1.tmp" "$1"
}

# Create the autolock for a given version
generate_autolock() {
    local version=$1; shift
    local autolockfile="${tmprepo}/tmp/manifest-autolock-${version}.json"
    if [ ! -f "${autolockfile}" ]; then
        # Just use the first arch available. In practice, this'll likely always be
        # x86_64, but it should work just as well if not.
        local lockfile
        lockfile=$(find "${workdir}/builds/${version}/" -name 'manifest-lock.generated.*.json' -print -quit 2>/dev/null)
        if [ -z "${lockfile}" ]; then
            # no generated lockfiles found; we can't autolock
            return
        fi
        cp "${lockfile}" "${autolockfile}"
        strip_out_lockfile_arches "${autolockfile}"
    fi
    echo "${autolockfile}"
}

json_key() {
    jq -r ".[\"$1\"]" < "${builddir}/meta.json"
}

meta_key() {
    cosa meta --workdir="${workdir}" --build="${build:-latest}" --get-value "${@}"
}

# runvm generates and runs a minimal VM which we use to "bootstrap" our build
# process.  It mounts the workdir via virtiofs.  If you need to add new packages into
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

    # tmp_builddir may or may not already be set by the caller.
    # shellcheck disable=SC2086
    if [ -z "${tmp_builddir:-}" ]; then
        tmp_builddir="$(mktemp -p ${workdir}/tmp -d supermin.XXXX)"
        export tmp_builddir
        local cleanup_tmpdir=1
    fi

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
    cat <<EOF >> "${vmpreparedir}/hostfiles"
/usr/lib/osbuild/stages/org.osbuild.bfb
/usr/lib/osbuild/stages/org.osbuild.bfb.meta.json
EOF

    # and include all GPG keys
    echo '/etc/pki/rpm-gpg/*' >> "${vmpreparedir}/hostfiles"

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
# - tee to the virtio port so its output is also part of the supermin output in
#   case e.g. a key msg happens in dmesg when the command does a specific operation.
# - Use a subshell because otherwise init will use workdir as its cwd and we won't
#   be able to unmount the virtiofs mount cleanly. This leads to consistency issues.
if [ -z "${RUNVM_SHELL:-}" ]; then
  (cd ${workdir}; bash ${tmp_builddir}/cmd.sh |& tee /dev/virtio-ports/cosa-cmdout) || rc=\$?
else
  (cd ${workdir}; RUNVM_SHELL=${RUNVM_SHELL:-} bash)
fi
echo \$rc > ${rc_file}
if [ -n "\${cachedev}" ]; then
    # Sometimes if failures occur the containers storage stack will leave
    # around some overlayfs mounts so let's recursively unmount here.
    # I wasn't able to reproduce this outside of our supermin VM setup so
    # didn't report this issue upstream.
    if mountpoint -q ${workdir}/cache/cache-containers-storage/storage/overlay; then
        umount -R ${workdir}/cache/cache-containers-storage/storage/overlay
    fi

    # Recover any unused space and attempt to safely umount the cache
    /sbin/fstrim -v ${workdir}/cache
    mount -o remount,ro ${workdir}/cache
    fsfreeze -f ${workdir}/cache
    fsfreeze -u ${workdir}/cache
    umount -R ${workdir}/cache
fi
umount ${workdir}
/sbin/reboot -f
EOF
    chmod a+x "${vmpreparedir}"/init
    (cd "${vmpreparedir}" && tar -czf init.tar.gz --remove-files init)
    # put the supermin output in a separate file since it's noisy
    if ! supermin --build "${vmpreparedir}" --size 10G -f ext2 -o "${vmbuilddir}" \
            &> "${tmp_builddir}/supermin.out"; then
        cat "${tmp_builddir}/supermin.out"
        fatal "Failed to run: supermin --build"
    fi
    superminrootfsuuid=$(blkid --output=value --match-tag=UUID "${vmbuilddir}/root")

    # this is the command run in the supermin container
    # we hardcode a umask of 0022 here to make sure that composes are run
    # with a consistent value, regardless of the environment
    echo "umask 0022" > "${tmp_builddir}"/cmd.sh
    for arg in "$@"; do
        # escape it appropriately so that spaces in args survive
        printf '%q ' "$arg" >> "${tmp_builddir}"/cmd.sh
    done

    touch "${runvm_console}"

    # There seems to be some false positives in shellcheck
    # https://github.com/koalaman/shellcheck/issues/2217
    memory_default=3072
    # shellcheck disable=2031
    case $arch in
    # Power 8 page faults with 2G of memory in rpm-ostree
    # Most probably due to radix and 64k overhead.
    "ppc64le") memory_default=4096 ;;
    esac

    kola_args=(kola qemuexec -m "${COSA_SUPERMIN_MEMORY:-${memory_default}}" --auto-cpus -U --workdir none \
               --console-to-file "${runvm_console}" --bind-rw "${workdir},workdir")

    base_qemu_args=(-drive 'if=none,id=root,format=raw,snapshot=on,file='"${vmbuilddir}"'/root,index=1' \
                    -device 'virtio-blk,drive=root' \
                    -kernel "${vmbuilddir}/kernel" -initrd "${vmbuilddir}/initrd" \
                    -no-reboot -nodefaults \
                    -device virtio-serial \
                    -append "root=UUID=${superminrootfsuuid} console=${DEFAULT_TERMINAL} selinux=1 enforcing=0 autorelabel=1" \
                   )

    # support local dev cases where src/config is a symlink.  Note if you change or extend to this set,
    # you also need to update supermin-init-prelude.sh to mount it inside the VM.
    for maybe_symlink in "${workdir}"/{src/config,src/yumrepos}; do
        if [ -L "${maybe_symlink}" ]; then
            local bn
            bn=$(basename "${maybe_symlink}")
            kola_args+=("--bind-ro" "${maybe_symlink},/cosa/src/${bn}")
        fi
    done

    if [ -z "${RUNVM_SHELL:-}" ]; then
        if ! "${kola_args[@]}" -- "${base_qemu_args[@]}" \
            -device virtserialport,chardev=virtioserial0,name=cosa-cmdout \
            -chardev stdio,id=virtioserial0 \
            "${qemu_args[@]}" < /dev/zero; then # qemu hangs if it has nothing to read on stdin
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

    if [ -n "${cleanup_tmpdir:-}" ]; then
        rm -rf "${tmp_builddir}"
        unset tmp_builddir
    fi

    return "${rc}"
}

prepare_git_artifacts() {
    # prepare_git_artifacts prepares two artifacts from a GIT repo:
    #   1. JSON describing the GIT tree.
    #   2. Optionally, a tarball of the source.
    local gitd="${1:?first argument must be the git directory}"; shift;
    local json="${1:?second argument must be the json file name to emit}"; shift;
    local tarball="${1:-}"

    local is_dirty="false"
    local head_ref="unknown"
    local head_remote="unknown"
    local head_url="unknown"
    local gc="git --work-tree=${gitd} --git-dir=${gitd}/.git"

    # shellcheck disable=SC2086
    if ! ${gc} diff --quiet --exit-code; then
        is_dirty="true"
    fi

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

    # shellcheck disable=SC2046 disable=SC2086
    cat > "${json}" <<EOC
{
    "date": "$(date -u +$RFC3339)",
    "git": {
        "commit": "${rev}",
        "origin": "${head_url}",
        "branch": "${branch}",
        "dirty": "${is_dirty}"
    }
}
EOC

    if [ -n "$tarball" ]; then
        tar -C "${gitd}" -czf "${tarball}" --exclude-vcs .
        chmod 0444 "${tarball}"

        local checksum name size
        checksum=$(sha256sum "${tarball}" | awk '{print$1}')
        name=$(basename "${tarball}")
        size=$(find "${tarball}" -printf %s)

        # Add file entry to json
        jq -s add "${json}" - > "${json}.new" <<EOC
{
    "file": {
        "checksum": "${checksum}",
        "checksum_type": "sha256",
        "format": "tar.gz",
        "name": "${name}",
        "size": "${size}"
    }
}
EOC
        mv "${json}.new" "${json}"
    fi

    chmod 0444 "${json}"
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

# API to prepare image builds.
# Ensures that the tmp/repo ostree repo is initialized,
# and also writes tmp/image.json if arg2 is unset or set to 1
import_ostree_commit_for_build() {
    local buildid=$1; shift
    local extractjson=${1:-1}
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib import cmdlib
from cosalib.builds import Builds
workdir = '${workdir:-$(pwd)}'
builds = Builds(workdir)
builddir = builds.get_build_dir('${buildid}')
buildmeta = builds.get_build_meta('${buildid}')
cmdlib.import_ostree_commit(workdir, builddir, buildmeta, extract_json=('${extractjson}' == '1'))
")
}
