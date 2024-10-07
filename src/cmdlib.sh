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

# Execute a command, also writing the cmdline to stdout
runv() {
    echo "Running: " "$@"
    "$@"
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

# Use this for things like disabling fsync.
# For more information, see the docs of `cosa init --transient`.
is_transient() {
    test -f "${workdir}"/tmp/cosa-transient
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
    python3 -c 'import sys, json, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout, sort_keys=True)' < "$1" > "$2"
}

prepare_build() {
    preflight
    preflight_kvm
    workdir="$(pwd)"

    if ! [ -d builds ]; then
        fatal "No ${workdir}/builds found; did you run coreos-assembler init?"
    fi

    if [ "$(stat -f --printf="%T" .)" = "overlayfs" ] && [ -z "${COSA_SKIP_OVERLAY:-}" ]; then
        fatal "${workdir} must be a volume"
    fi

    if test '!' -w "${workdir}"; then
        ls -ald "${workdir}"
        fatal "${workdir} is not writable"
    fi

    # Be nice to people who have older versions that
    # didn't create this in `init`.
    mkdir -p "${workdir}"/tmp

    # Allocate temporary space for this build
    export tmp_builddir="${workdir}/tmp/build${IMAGE_TYPE:+.$IMAGE_TYPE}"
    rm "${tmp_builddir}" -rf
    mkdir "${tmp_builddir}"

    configdir=${workdir}/src/config
    initconfig="${workdir}/src/config.json"
    if [[ -f "${initconfig}" ]]; then
        variant="$(jq --raw-output '."coreos-assembler.config-variant"' "${initconfig}")"
        manifest="${configdir}/manifest-${variant}.yaml"
        image="${configdir}/image-${variant}.yaml"
        # Currently unused as cmd-buildextend-extensions is in Python
        # extensions="${configdir}/extensions-${variant}.yaml"
    else
        manifest="${configdir}/manifest.yaml"
        image="${configdir}/image.yaml"
        # Currently unused as cmd-buildextend-extensions is in Python
        # extensions="${configdir}/extensions.yaml"
    fi
    # for the base lockfile, we default to JSON since that's what rpm-ostree
    # actually outputs
    manifest_lock=$(pick_yaml_or_else_json "${configdir}/manifest-lock.${basearch}" json)
    manifest_lock_overrides=$(pick_yaml_or_else_json "${configdir}/manifest-lock.overrides")
    manifest_lock_arch_overrides=$(pick_yaml_or_else_json "${configdir}/manifest-lock.overrides.${basearch}")
    fetch_stamp="${workdir}"/cache/fetched-stamp

    # We also need the platform.yaml as JSON
    platforms_yaml="${configdir}/platforms.yaml"
    platforms_json="${tmp_builddir}/platforms.json"
    yaml2json "${platforms_yaml}" "${platforms_json}.all"
    # Copy platforms table if it's non-empty for this arch
    if jq -e ".$basearch" < "$platforms_json.all" > /dev/null; then
        jq ".$basearch" < "$platforms_json.all" > "${platforms_json}"
    fi

    export image_json="${tmp_builddir}/image.json"
    write_image_json "${image}" "${image_json}"
    # These need to be absolute paths right now for rpm-ostree
    composejson="$(readlink -f "${workdir}"/tmp/compose.json)"
    export composejson

    export workdir configdir manifest manifest_lock manifest_lock_overrides manifest_lock_arch_overrides
    export fetch_stamp

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
            # This archive repo is transient, so lower the compression
            # level to avoid burning excessive CPU.
            ostree --repo="${tmprepo}" config set archive.zlib-level 2
        fi

        # No need to fsync for transient flows
        if test -f "${workdir}/tmp/cosa-transient"; then
            ostree --repo="${tmprepo}" config set 'core.fsync' 'false'
        fi
    fi

    configdir_gitrepo=${configdir}
    if [ -e "${workdir}/src/config-git" ]; then
        configdir_gitrepo="${workdir}/src/config-git"
    fi
    export configdir_gitrepo

    flattened_manifest=${tmp_builddir}/manifest.json
    rpm-ostree compose tree --repo="${tmprepo}" --print-only "${manifest}" > "${flattened_manifest}"
    export flattened_manifest

    # Abuse the rojig/name as the name of the VM images
    # Also grab rojig summary for image upload descriptions
    name=$(jq -r '.rojig.name' < "${flattened_manifest}")
    summary=$(jq -r '.rojig.summary' < "${flattened_manifest}")
    ref=$(jq -r '.ref//""' < "${flattened_manifest}")
    export name ref summary
    # And validate fields coreos-assembler requires, but not rpm-ostree
    required_fields=("automatic-version-prefix")
    for field in "${required_fields[@]}"; do
        if ! jq -re '."'"${field}"'"' < "${flattened_manifest}" >/dev/null; then
            echo "Missing required field in src/config/manifest.yaml: ${field}" 1>&2
            exit 1
        fi
    done

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
    ostree commit --repo="${tmprepo}" \
        --tree=dir="${TMPDIR}/overlay" -b "overlay/${name}" \
        --owner-uid 0 --owner-gid 0 --no-xattrs --no-bindings --parent=none \
        --mode-ro-executables --timestamp "${git_timestamp}" \
        --statoverride <(sed -e '/^#/d' "${TMPDIR}/overlay/statoverride") \
        --skip-list <(echo /statoverride)
}

create_content_manifest(){
    local source_file=$1
    local destination=$2
    mkdir -p "${workdir}"/tmp/buildinfo
    base_repos=$(jq .repos "${flattened_manifest}")

    # Get the data form content_sets.yaml and map the repo names given in '$base_repos' to their corresponding
    # pulp repository IDs provided in https://www.redhat.com/security/data/metrics/repository-to-cpe.json
    python3 -c "
import json, yaml;

# Open the yaml and load the data
f = open('$source_file')
data = yaml.safe_load(f);
f.close();
repos=[];

for base_repo in $base_repos:
    if base_repo in data['repo_mapping']:
        if data['repo_mapping'][base_repo]['name'] != '':
            repo_name = data['repo_mapping'][base_repo]['name'].replace('\$ARCH', '$(arch)');
            repos.append(repo_name)
        else:
            print('Warning: No corresponding repo in repository-to-cpe.json for ' + base_repo)
    else:
        # Warning message for repositories with no entry in content_sets.yaml
        print('Warning: No corresponding entry in content_sets.yaml for ' + base_repo)

content_manifest_data = json.dumps({
    'metadata': {
        'icm_version': 1,
        'icm_spec': 'https://raw.githubusercontent.com/containerbuildsystem/atomic-reactor/master/atomic_reactor/schemas/content_manifest.json',
        'image_layer_index': 1
    },
    'content_sets': repos,
    'image_contents': []
    });
with open('$destination', 'w') as outfile:
    outfile.write(content_manifest_data)
    "
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

    if [ -d "${overridesdir}" ] || [ -d "${ovld}" ] || [ -d "${workdir}/src/yumrepos" ]; then
        mkdir -p "${tmp_overridesdir}"
        cat > "${override_manifest}" <<EOF
include: ${manifest}
EOF
        # Because right now rpm-ostree doesn't look for .repo files in
        # each included dir.
        # https://github.com/projectatomic/rpm-ostree/issues/1628
        find "${configdir}/" -maxdepth 1 -type f -name '*.repo' -exec cp -t "${tmp_overridesdir}" {} +
        if [ -d "${workdir}/src/yumrepos" ]; then
            find "${workdir}/src/yumrepos/" -maxdepth 1 -type f -name '*.repo' -exec cp -t "${tmp_overridesdir}" {} +
        fi
        if ! ls "${tmp_overridesdir}"/*.repo; then
            echo "ERROR: no yum repo files were found"
            exit 1
        fi
        manifest=${override_manifest}
    fi

    if [ -d "${ovld}" ]; then
        for n in "${ovld}"/*; do
            if ! [ -d "${n}" ]; then
                continue
            fi
            commit_overlay "$(basename "${n}")" "${n}"
        done
    fi

    # Store the fully rendered disk image config (image.json)
    # and the platform (platforms.json) if it exists inside
    # the ostree commit, so it can later be extracted by disk image
    # builds. Also the full contents of the live/ directory.
    local usr_share_cosa="${tmp_overridesdir}/usr-share-cosa"
    mkdir -p "${usr_share_cosa}/usr/share/coreos-assembler/"
    cp "${image_json}" "${usr_share_cosa}/usr/share/coreos-assembler/"
    if [ -f "${platforms_json}" ]; then
        cp "${platforms_json}" "${usr_share_cosa}/usr/share/coreos-assembler/"
    fi
    cp -r "${configdir}/live" "${usr_share_cosa}/usr/share/coreos-assembler/live"
    commit_overlay usr-share-cosa "${usr_share_cosa}"
    layers="${layers} overlay/usr-share-cosa"

    local_overrides_lockfile="${tmp_overridesdir}/local-overrides.json"
    if [ -n "${with_cosa_overrides}" ] && [[ -n $(ls "${overridesdir}/rpm/"*.rpm 2> /dev/null) ]]; then
        (cd "${overridesdir}"/rpm && rm -rf .repodata && createrepo_c .)
        # synthesize an override lockfile to force rpm-ostree to pick up our
        # override RPMS -- we try to be nice here and allow multiple versions of
        # the same RPMs: the `dnf repoquery` below is to pick the latest one
        dnf repoquery  --repofrompath=tmp,"file://${overridesdir}/rpm" \
            --disablerepo '*' --enablerepo tmp --refresh --latest-limit 1 \
            --exclude '*.src' --qf '%{name}\t%{evr}\t%{arch}' \
            --quiet > "${tmp_overridesdir}/pkgs.txt"

        # shellcheck disable=SC2002
        cat "${tmp_overridesdir}/pkgs.txt" | python3 -c '
import sys, json
lockfile = {"packages": {}}
for line in sys.stdin:
    name, evr, arch = line.strip().split("\t")
    lockfile["packages"][name] = {"evra": f"{evr}.{arch}"}
json.dump(lockfile, sys.stdout)' > "${local_overrides_lockfile}"

        # for all the repo packages in the manifest for which we have an
        # override, create a new repo-packages entry to make sure our overrides
        # win.
        # shellcheck disable=SC2002
        cat "${tmp_overridesdir}/pkgs.txt" | python3 -c "
import sys, yaml
flattened = yaml.safe_load(open('${flattened_manifest}'))
all_overrides = set()
for line in sys.stdin:
    all_overrides.add(line.strip().split('\t')[0])
repo_overrides = set()
for repopkg in flattened.get('repo-packages', []):
    repo_overrides.update(all_overrides.intersection(set(repopkg['packages'])))
manifest = {
    'repos': ['coreos-assembler-local-overrides'],
    'repo-packages': [{
        'repo': 'coreos-assembler-local-overrides',
        'packages': list(repo_overrides)
    }]
}
yaml.dump(manifest, sys.stdout)" >> "${override_manifest}"
        rm "${tmp_overridesdir}/pkgs.txt"

        echo "Using RPM overrides from: ${overridesdir}/rpm"
        touch "${overrides_active_stamp}"
        cat > "${tmp_overridesdir}"/coreos-assembler-local-overrides.repo <<EOF
[coreos-assembler-local-overrides]
name=coreos-assembler-local-overrides
baseurl=file://${workdir}/overrides/rpm
gpgcheck=0
cost=500
module_hotfixes=true
EOF
    else
        rm -vf "${local_overrides_lockfile}"
    fi

    contentset_path=""
    if [ -e "${configdir}/content_sets.yaml" ]; then
        contentset_path="${configdir}/content_sets.yaml"
    elif [ -e "${workdir}/src/yumrepos/content_sets.yaml" ]; then
        contentset_path="${workdir}/src/yumrepos/content_sets.yaml"
    fi

    if [ -n "${contentset_path}" ]; then
        mkdir -p "${tmp_overridesdir}"/contentsetrootfs/usr/share/buildinfo/
        # create_content_manifest takes in the base repos and maps them to their pulp repository IDs
        # available in content_sets.yaml. The mapped repos are then available in content_manifest.json
        # Feature: https://issues.redhat.com/browse/GRPA-3731
        create_content_manifest "${contentset_path}" "${tmp_overridesdir}/contentsetrootfs/usr/share/buildinfo/content_manifest.json"
        # adjust permissions to appease the ext.config.shared.files.file-directory-permissions test
        chmod 0644 "${tmp_overridesdir}/contentsetrootfs/usr/share/buildinfo/content_manifest.json"

        echo -n "Committing ${tmp_overridesdir}/contentsetrootfs... "
        commit_overlay contentset "${tmp_overridesdir}/contentsetrootfs"
        layers="${layers} overlay/contentset"
    fi

    if [ -n "${layers}" ]; then
        echo "ostree-layers:" >> "${override_manifest}"
        for layer in ${layers}; do
            echo "  - ${layer}" >> "${override_manifest}"
        done
    fi

    rootfs_overrides="${overridesdir}/rootfs"
    if [[ -d "${rootfs_overrides}" && -n $(ls -A "${rootfs_overrides}") ]]; then
        touch "${overrides_active_stamp}"
        commit_overlay cosa-overrides-rootfs "${rootfs_overrides}"
          cat >> "${override_manifest}" << EOF
ostree-override-layers:
  - overlay/cosa-overrides-rootfs
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

    local workdir=${workdir:-$(pwd)}
    local repo=${tmprepo:-${workdir}/tmp/repo}

    rm -f "${changed_stamp}"
    # shellcheck disable=SC2086
    set - ${COSA_RPMOSTREE_GDB:-} rpm-ostree compose tree \
            --touch-if-changed "${changed_stamp}" --cachedir="${workdir}"/cache \
            ${COSA_RPMOSTREE_ARGS:-} --unified-core "${manifest}" "$@"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        set - "$@" --repo "${repo}" --write-composejson-to "${composejson}"
        # we hardcode a umask of 0022 here to make sure that composes are run
        # with a consistent value, regardless of the environment
        (umask 0022 && sudo -E "$@")
        sudo chown -R -h "${USER}":"${USER}" "${tmprepo}"
    else
        runvm_with_cache -- "$@" --repo "${repo}" --write-composejson-to "${composejson}"
    fi
}

runcompose_extensions() {
    local outputdir=$1; shift
    local workdir=${workdir:-$(pwd)}
    local repo=${tmprepo:-${workdir}/tmp/repo}

    rm -f "${changed_stamp}"
    # shellcheck disable=SC2086
    set - ${COSA_RPMOSTREE_GDB:-} rpm-ostree compose extensions --repo="${repo}" \
            --touch-if-changed "${changed_stamp}" --cachedir="${workdir}"/cache \
            ${COSA_RPMOSTREE_ARGS:-} "$@" --output-dir "$outputdir"

    echo "Running: $*"

    # this is the heart of the privs vs no privs dual path
    if has_privileges; then
        # we hardcode a umask of 0022 here to make sure that composes are run
        # with a consistent value, regardless of the environment
        (umask 0022 && sudo -E "$@")
        sudo chown -R -h "${USER}":"${USER}" "${outputdir}"
    else
        # Use a snapshot version of the cache qcow2 to allow multiple users
        # of the cache at the same time. This is needed because the extensions
        # and other artifacts are built in parallel.
        local snapshot='on'
        runvm_with_cache_snapshot "${snapshot}" -- "$@"
    fi
}

# Run with cache disk with optional snapshot=on, which means no changes get written back to
# the cache disk. `runvm_with_cache_snapshot on` will set snapshotting to on.
runvm_with_cache_snapshot() {
    local snapshot=$1; shift
    local cache_size=${RUNVM_CACHE_SIZE:-20G}
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
    cache_args+=("-drive" "if=none,id=cache,discard=unmap,snapshot=${snapshot},file=${workdir}/cache/cache2.qcow2" \
                        "-device" "virtio-blk,drive=cache")
    runvm "${cache_args[@]}" "$@"
}

runvm_with_cache() {
    local snapshot='off'
    runvm_with_cache_snapshot $snapshot "$@"
}

# Strips out the digest field from lockfiles since they subtly conflict with
# various workflows.
strip_out_lockfile_digests() {
    jq 'del(.packages[].digest)' "$1" > "$1.tmp"
    mv "$1.tmp" "$1"
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

    # tmp_builddir is set in prepare_build, but some stages may not
    # know that it exists.
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
    echo /usr/lib/osbuild/stages/org.osbuild.dmverity >> "${vmpreparedir}/hostfiles"
    echo /usr/lib/osbuild/stages/org.osbuild.coreos.live-iso >> "${vmpreparedir}/hostfiles"
    echo /usr/lib/osbuild/stages/org.osbuild.coreos.live-iso.meta.json >> "${vmpreparedir}/hostfiles"

    # and include all GPG keys
    find /etc/pki/rpm-gpg/ -type f >> "${vmpreparedir}/hostfiles"

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
  (cd ${workdir}; bash)
fi
echo \$rc > ${rc_file}
if [ -n "\${cachedev}" ]; then
    /sbin/fstrim -v ${workdir}/cache
    mount -o remount,ro ${workdir}/cache
    fsfreeze -f ${workdir}/cache
    fsfreeze -u ${workdir}/cache
    umount ${workdir}/cache
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
    memory_default=2048
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
    #   2. Optionally, a tarball of the source.
    local gitd="${1:?first argument must be the git directory}"; shift;
    local json="${1:?second argument must be the json file name to emit}"; shift;
    local tarball="${1:-}"

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

get_latest_build_for_arch() {
    local arch=$1; shift
    # yup, this is happening
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib.builds import Builds
buildid = Builds('${workdir:-$(pwd)}').get_latest_for_arch('${arch}')
if buildid:
    print(buildid)")
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

# Prepare the image.json as part of an ostree image build
write_image_json() {
    local srcfile=$1; shift
    local outfile=$1; shift
    (python3 -c "
import sys
sys.path.insert(0, '${DIR}')
from cosalib import cmdlib
cmdlib.write_image_json('${srcfile}', '${outfile}')")
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
cmdlib.import_ostree_commit(workdir, builddir, buildmeta, ${extractjson})
")
}

# Extract the value of NAME from os-release
extract_osrelease_name() {
    local buildid=$1; shift
    local out="$workdir/tmp/osrelease"
    rm "${out}" -rf
    ostree checkout --repo "${tmprepo}" --user-mode --subpath=/usr/lib/os-release "${buildid}" "$out"
    # shellcheck disable=SC1091,SC2153
    (. "$out/os-release" && echo "${NAME}")
}
