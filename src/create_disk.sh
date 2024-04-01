#!/bin/bash
set -euo pipefail

# This script is run in supermin to create a Fedora CoreOS style
# disk image, very much in the spirit of the original
# Container Linux (orig CoreOS) disk layout, although adapted
# for OSTree, and using XFS for /, among other things.
# Some more background in https://github.com/coreos/fedora-coreos-tracker/issues/18
# The layout is intentionally not very configurable at this time,
# although see also https://github.com/coreos/coreos-assembler/pull/298
# For people building "derived"/custom FCOS-like systems, feel free to file
# an issue and we can discuss configuration needs.

# This fixed UUID is detected in ignition-dracut and changed
# on firstboot:
# https://github.com/coreos/ignition-dracut/blob/6136be3d9d38d7926a61cd4d1b4ba5f9baf0892f/dracut/30ignition/coreos-gpt-setup.sh#L7
uninitialized_gpt_uuid="00000000-0000-4000-a000-000000000001"
# These UUIDs should be changed by code in fedora-coreos-config on firstboot, see
# https://github.com/coreos/fedora-coreos-tracker/issues/465
bootfs_uuid="96d15588-3596-4b3c-adca-a2ff7279ea63"
rootfs_uuid="910678ff-f77e-4a7d-8d53-86f2ac47a823"
deploy_root=

usage() {
    cat <<EOC
${0} creates a supermin virtual machine to create a
Fedora CoreOS style disk image from an OSTree.

Options:
    --config: JSON-formatted image.yaml
    --help: show this help
    --platform: Ignition platform ID
    --platforms-json: platforms.yaml in JSON format
    --no-x86-bios-bootloader: don't install BIOS bootloader on x86_64
    --with-secure-execution:  enable IBM SecureExecution

You probably don't want to run this script by hand. This script is
run as part of 'coreos-assembler build'.
EOC
}

config=
disk=
platform=metal
platforms_json=
secure_execution=0
ignition_pubkey=
x86_bios_bootloader=1

while [ $# -gt 0 ];
do
    flag="${1}"; shift;
    case "${flag}" in
        --config)                   config="${1}"; shift;;
        --help)                     usage; exit;;
        --no-x86-bios-bootloader)   x86_bios_bootloader=0;;
        --platform)                 platform="${1}"; shift;;
        --platforms-json)           platforms_json="${1}"; shift;;
        --with-secure-execution)    secure_execution=1;;
        --write-ignition-pubkey-to) ignition_pubkey="${1}"; shift;;
         *) echo "${flag} is not understood."; usage; exit 10;;
     esac;
done

udevtrig() {
    udevadm trigger
    udevadm settle
}

export PATH=$PATH:/sbin:/usr/sbin
arch="$(uname -m)"

if [ -z "$platforms_json" ]; then
    echo "Missing --platforms-json" >&2
    exit 1
fi
# just copy it over to /tmp and work from there to minimize virtiofs I/O
cp "${platforms_json}" /tmp/platforms.json
platforms_json=/tmp/platforms.json
platform_grub_cmds=$(jq -r ".${arch}.${platform}.grub_commands // [] | join(\"\\\\n\")" < "${platforms_json}")
platform_kargs=$(jq -r ".${arch}.${platform}.kernel_arguments // [] | join(\" \")" < "${platforms_json}")

disk=$(realpath /dev/disk/by-id/virtio-target)

config="${config:?--config must be defined}"

# https://github.com/coreos/coreos-assembler/pull/2480
dump_err_info () {
    lsblk -f || true
}
trap dump_err_info ERR

# Parse the passed config JSON and extract a mandatory value
getconfig() {
    k=$1
    jq -re .\""$k"\" < "${config}"
}
# Return a configuration value, or default if not set
getconfig_def() {
    k=$1
    shift
    default=$1
    shift
    jq -re .\""$k"\"//\""${default}"\" < "${config}"
}

rootfs_type=$(getconfig rootfs)
case "${rootfs_type}" in
    xfs|ext4verity|btrfs) ;;
    *) echo "Invalid rootfs type: ${rootfs_type}" 1>&2; exit 1;;
esac
rootfs_args=$(getconfig_def "rootfs-args" "")

bootfs=$(getconfig "bootfs")
composefs=$(getconfig_def "composefs" "")
grub_script=$(getconfig "grub-script")
ostree_container=$(getconfig "ostree-container")
ostree_container_spec="ostree-unverified-image:oci-archive:${ostree_container}"
commit=$(getconfig "ostree-commit")
ref=$(getconfig "ostree-ref")
# We support not setting a remote name (used by RHCOS)
remote_name=$(getconfig_def "ostree-remote" "")
deploy_via_container=$(getconfig "deploy-via-container" "")
container_imgref=$(getconfig "container-imgref" "")
os_name=$(getconfig "osname")
buildid=$(getconfig "buildid")
imgid=$(getconfig "imgid")
extra_kargs=$(getconfig "extra-kargs-string" "")

# populate remaining kargs
extra_kargs+=" ignition.platform.id=${platform}"
if [ -n "${platform_kargs}" ]; then
    extra_kargs+=" ${platform_kargs}"
fi

set -x

# Partition and create fs's. The 0...4...a...1 uuid is a sentinal used by coreos-gpt-setup
# in ignition-dracut. It signals that the disk needs to have it's uuid randomized and the
# backup header moved to the end of the disk.
# Pin /se, /boot and / to the partition number 1, 3 and 4 respectively. Also insert reserved
# partitions on aarch64/ppc64le to keep the 1,2,3,4 partition numbers aligned across
# x86_64/aarch64/ppc64le. We decided not to try to achieve partition parity on s390x
# because a bare metal install onto an s390x DASD translates the GPT to DASD partitions
# and we only get three of those. https://github.com/coreos/fedora-coreos-tracker/issues/855
BOOTPN=3
ROOTPN=4
if [[ ${secure_execution} -eq 1 ]]; then
    SDPART=1
    BOOTVERITYHASHPN=5
    ROOTVERITYHASHPN=6
    extra_kargs="${extra_kargs} swiotlb=262144"
fi
# shellcheck disable=SC2031
case "$arch" in
    x86_64)
        EFIPN=2
        sgdisk -Z "$disk" \
        -U "${uninitialized_gpt_uuid}" \
        -n 1:0:+1M -c 1:BIOS-BOOT -t 1:21686148-6449-6E6F-744E-656564454649 \
        -n ${EFIPN}:0:+127M -c ${EFIPN}:EFI-SYSTEM -t ${EFIPN}:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:0 -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        ;;
    aarch64)
        RESERVEDPN=1
        EFIPN=2
        sgdisk -Z "$disk" \
        -U "${uninitialized_gpt_uuid}" \
        -n ${RESERVEDPN}:0:+1M -c ${RESERVEDPN}:reserved -t ${RESERVEDPN}:8DA63339-0007-60C0-C436-083AC8230908 \
        -n ${EFIPN}:0:+127M -c ${EFIPN}:EFI-SYSTEM -t ${EFIPN}:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:0 -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        ;;
    s390x)
        sgdisk_args=()
        rootp_end=0
        if [[ ${secure_execution} -eq 1 ]]; then
            # shellcheck disable=SC2206
            sgdisk_args+=(-n ${SDPART}:0:+200M -c ${SDPART}:se -t ${SDPART}:0FC63DAF-8483-4772-8E79-3D69D8477DE4)
            # we need to leave space for the verity hash partitions (and add 1MB otherwise sgdisk can't fit them for some reason)
            rootp_end=-$((128+256+1))M
        fi

        # shellcheck disable=SC2206
        sgdisk_args+=(-n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
                      -n ${ROOTPN}:0:${rootp_end} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4)
        if [[ ${secure_execution} -eq 1 ]]; then
            # note these length values are hardcoded in both rootp_end above and in `cmd-buildextend-metal`
            # shellcheck disable=SC2206
            sgdisk_args+=(-n ${BOOTVERITYHASHPN}:0:+128M -c ${BOOTVERITYHASHPN}:boothash \
                          -n ${ROOTVERITYHASHPN}:0:+256M -c ${ROOTVERITYHASHPN}:roothash)
        fi
        # NB: in the bare metal case when targeting ECKD DASD disks, this
        # partition table is not what actually gets written to disk in the end:
        # coreos-installer has code which transforms it into a DASD-compatible
        # partition table and copies each partition individually bitwise.
        sgdisk -Z "$disk" -U "${uninitialized_gpt_uuid}" "${sgdisk_args[@]}"
        ;;
    ppc64le)
        PREPPN=1
        RESERVEDPN=2
        # ppc64le doesn't use special uuid for root partition
        sgdisk -Z "$disk" \
        -U "${uninitialized_gpt_uuid}" \
        -n ${PREPPN}:0:+4M -c ${PREPPN}:PowerPC-PReP-boot -t ${PREPPN}:9E1A2D38-C612-4316-AA26-8B49521E5A8B \
        -n ${RESERVEDPN}:0:+1M -c ${RESERVEDPN}:reserved -t ${RESERVEDPN}:8DA63339-0007-60C0-C436-083AC8230908 \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:0 -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        ;;
esac

sgdisk -p "$disk"
udevtrig

boot_dev="${disk}${BOOTPN}"
root_dev="${disk}${ROOTPN}"

bootargs=
# Detect if the target system supports orphan_file.
# https://github.com/coreos/coreos-assembler/pull/3653#issuecomment-1813181723
# Ideally, we'd do feature detection here but there's no clean way to do that.
# So just use version comparisons. (But ideally ideally, we use the mkfs.*
# binaries from the target system, not cosa.)
e2fsprogs_version=$(jq -r '.["rpmostree.rpmdb.pkglist"][] | select(.[0] == "e2fsprogs") | .[2]' \
                        "builds/${buildid}/${arch}/commitmeta.json")
if [ "${e2fsprogs_version}" != "1.47.0" ] && \
        [ "$(echo -e "${e2fsprogs_version}\n1.47.0" | sort -V | tail -n1)" = "1.47.0" ]; then
    # target system has e2fsprogs older than 1.47.0; it won't support
    # orphan_file so make sure we opt out of it
    bootargs+=" -O ^orphan_file"
fi

case "${bootfs}" in
    ext4verity)
        # Need blocks to match host page size; TODO
        # really mkfs.ext4 should know this.
        bootargs+=" -b $(getconf PAGE_SIZE) -O verity"
        ;;
    ext4) ;;
    *) echo "Unhandled bootfs: ${bootfs}" 1>&2; exit 1 ;;
esac

if [[ ${secure_execution} -eq 1 ]]; then
    # Unencrypted partition for sd-boot
    # shellcheck disable=SC2086
    mkfs.ext4 ${bootargs} "${disk}${SDPART}" -L se -U random
fi

# shellcheck disable=SC2086
mkfs.ext4 ${bootargs} "${boot_dev}" -L boot -U "${bootfs_uuid}"
udevtrig

if [ ${EFIPN:+x} ]; then
       mkfs.fat "${disk}${EFIPN}" -n EFI-SYSTEM
       # BIOS boot partition has no FS; it's for BIOS GRUB
       # partition $PREPPN has no FS; it's for PowerPC PReP Boot
fi
case "${rootfs_type}" in
    ext4verity)
        # As of today, xfs doesn't support verity, so we have a choice of fs-verity or reflinks.
        # Now, fs-verity doesn't in practice gain us a huge amount of security because
        # there are other "persistence vectors".  See
        # https://blog.verbum.org/2017/06/12/on-dm-verity-and-operating-systems/
        # https://github.com/coreos/rpm-ostree/issues/702
        # And reflinks are *very* useful for the container stack with overlayfs (and in general).
        # So basically, we're choosing performance over half-implemented security.
        # Eventually, we'd like both - once XFS gains verity (probably not too hard),
        # we could unconditionally enable it there.
        # shellcheck disable=SC2086
        mkfs.ext4 -b "$(getconf PAGE_SIZE)" -O verity -L root "${root_dev}" -U "${rootfs_uuid}" ${rootfs_args}
        ;;
    btrfs)
        # shellcheck disable=SC2086
        mkfs.btrfs -L root "${root_dev}" -U "${rootfs_uuid}" ${rootfs_args}
        ;;
    xfs|"")
        # shellcheck disable=SC2086
        mkfs.xfs "${root_dev}" -L root -m reflink=1 -m uuid="${rootfs_uuid}" ${rootfs_args}
        ;;
    *)
        echo "Unknown rootfs_type: $rootfs_type" 1>&2
        exit 1
        ;;
esac
udevtrig

# since we normally run in supermin container and we need
# support parallel runs, use /tmp
rootfs=/tmp/rootfs

# mount the partitions
rm -rf ${rootfs}
mkdir -p ${rootfs}
mount -o discard "${root_dev}" ${rootfs}
chcon "$(matchpathcon -n /)" ${rootfs}
mkdir ${rootfs}/boot
chcon "$(matchpathcon -n /boot)" $rootfs/boot
mount "${boot_dev}" $rootfs/boot
chcon "$(matchpathcon -n /boot)" $rootfs/boot
# FAT doesn't support SELinux labeling, it uses "genfscon", so we
# don't need to give it a label manually.
if [ ${EFIPN:+x} ]; then
    mkdir ${rootfs}/boot/efi
    mount "${disk}${EFIPN}" $rootfs/boot/efi
fi
if [[ ${secure_execution} -eq 1 ]]; then
    mkdir ${rootfs}/se
    chcon "$(matchpathcon -n /boot)" $rootfs/se
fi

# Now that we have the basic disk layout, initialize the basic
# OSTree layout, load in the ostree commit and deploy it.
ostree admin init-fs --modern $rootfs
# May be overridden below (e.g. s390x)
ostree config --repo $rootfs/ostree/repo set sysroot.bootloader none
# Opt-in to https://github.com/ostreedev/ostree/pull/1767 AKA
# https://github.com/ostreedev/ostree/issues/1265
ostree config --repo $rootfs/ostree/repo set sysroot.readonly true
if test -n "${composefs}"; then
    ostree config --repo $rootfs/ostree/repo set ex-integrity.composefs true
fi
# Initialize the "stateroot"
ostree admin os-init "$os_name" --sysroot $rootfs

# Propagate flags into target repository
if [ "${rootfs_type}" = "ext4verity" ] && [ -z "${composefs}" ]; then
    ostree config --repo=$rootfs/ostree/repo set ex-fsverity.required 'true'
fi

# Compute kargs
allkargs="$extra_kargs"
# shellcheck disable=SC2031
if [ "$arch" != s390x ]; then
    # Note that $ignition_firstboot is interpreted by grub at boot time,
    # *not* the shell here.  Hence the backslash escape.
    allkargs+=" \$ignition_firstboot"
fi

if test -n "${deploy_via_container}"; then
    kargsargs=""
    for karg in $allkargs
    do
        kargsargs+="--karg=$karg "
    done
    # shellcheck disable=SC2086
    ostree container image deploy --imgref "${ostree_container_spec}" \
        ${container_imgref:+--target-imgref $container_imgref} \
        --write-commitid-to /tmp/commit.txt \
        --stateroot "$os_name" --sysroot $rootfs $kargsargs
    deploy_commit=$(cat /tmp/commit.txt)
    rm /tmp/commit.txt
else
    # Pull the container image...
    time ostree container image pull $rootfs/ostree/repo "${ostree_container_spec}"
    # But we default to not leaving a ref for the image around, so the
    # layers will get GC'd on the first update if the
    # user doesn't switch to a container image.
    ostree --repo=$rootfs/ostree/repo refs --delete ostree/container/image
    ostree --repo=$rootfs/ostree/repo prune --refs-only --depth=0
    # Deploy it, using an optional remote prefix
    if test -n "${remote_name}"; then
        deploy_ref="${remote_name}:${ref}"
        ostree refs --repo $rootfs/ostree/repo --create "${deploy_ref}" "${commit}"
    else
        deploy_ref=$commit
    fi
    kargsargs=""
    for karg in $allkargs
    do
        kargsargs+="--karg-append=$karg "
    done
    # shellcheck disable=SC2086
    ostree admin deploy "${deploy_ref}" --sysroot $rootfs --os "$os_name" $kargsargs
    deploy_commit=$commit
fi
# Sanity check
deploy_root="$rootfs/ostree/deploy/${os_name}/deploy/${deploy_commit}.0"
test -d "${deploy_root}" || (echo "failed to find $deploy_root"; exit 1)

# This will allow us to track the version that an install
# originally used; if we later need to understand something
# like "exactly what mkfs.xfs version was used" we can do
# that via looking at the upstream build and finding the
# build logs for it, getting the coreos-assembler version,
# and getting the `rpm -qa` from that.
#
# build:         The coreos-assembler build ID; today we support
#                having the same ostree commit in different image builds.
# ref:           The ostree ref used; useful for cross-checking.
# ostree-commit: Similar to `ref`; one can derive this from looking
#                at the coreos-assembler builds, but it's very
#                convenient to have here as a strong cross-reference.
# imgid:         The full image name, the same as will end up in the
#                `images` dict in `meta.json`.
cat > $rootfs/.coreos-aleph-version.json << EOF
{
	"build": "${buildid}",
	"ref": "${ref}",
	"ostree-commit": "${commit}",
	"imgid": "${imgid}"
}
EOF

install_uefi() {
    # https://github.com/coreos/fedora-coreos-tracker/issues/510
    # See also https://github.com/ostreedev/ostree/pull/1873#issuecomment-524439883
    chroot_run bootupctl backend install --src-root="/" "/sysroot"
    # We have a "static" grub config file that basically configures grub to look
    # in the RAID called "md-boot", if it exists, or the partition labeled "boot".
    local target_efi="$rootfs/boot/efi"
    local grubefi
    grubefi=$(find "${target_efi}/EFI/" -maxdepth 1 -type d | grep -v BOOT)
    local vendor_id="${grubefi##*/}"
    local vendordir="${target_efi}/EFI/${vendor_id}"
    mkdir -p "${vendordir}"
    cat > "${vendordir}/grub.cfg" << 'EOF'
if [ -e (md/md-boot) ]; then
  # The search command might pick a RAID component rather than the RAID,
  # since the /boot RAID currently uses superblock 1.0.  See the comment in
  # the main grub.cfg.
  set prefix=md/md-boot
else
  if [ -f ${config_directory}/bootuuid.cfg ]; then
    source ${config_directory}/bootuuid.cfg
  fi
  if [ -n "${BOOT_UUID}" ]; then
    search --fs-uuid "${BOOT_UUID}" --set prefix --no-floppy
  else
    search --label boot --set prefix --no-floppy
  fi
fi
set prefix=($prefix)/grub2
configfile $prefix/grub.cfg
boot
EOF
    install_grub_cfg
}

# copy the grub config and any other files we might need
install_grub_cfg() {
    # 0700 to match the RPM permissions which I think are mainly in case someone has
    # manually set a grub password
    mkdir -p $rootfs/boot/grub2
    chmod 0700 $rootfs/boot/grub2
    printf "%s\n" "$grub_script" | \
        sed -E 's@(^# CONSOLE-SETTINGS-START$)@\1'"${platform_grub_cmds:+\\n${platform_grub_cmds}}"'@' \
        > $rootfs/boot/grub2/grub.cfg
    # Copy platforms table if it's non-empty for this arch
    # shellcheck disable=SC2031
    if jq -e ".$arch" < "$platforms_json" > /dev/null; then
        mkdir -p "$rootfs/boot/coreos"
        jq ".$arch" < "$platforms_json" > "$rootfs/boot/coreos/platforms.json"
    fi
}

# For some commands, we need to make sure to use the binary and userspace of the
# target system. XXX: Switch to bwrap.
chroot_run() {
    for mnt in dev proc sys run var tmp; do
        mount --rbind "/$mnt" "${deploy_root}/$mnt"
    done
    mount --rbind "${rootfs}" "${deploy_root}/sysroot"
    chroot "${deploy_root}" "$@"
    umount --recursive "${deploy_root}/sysroot"
    for mnt in dev proc sys run var tmp; do
        umount --recursive "${deploy_root}/$mnt"
    done
}

generate_gpgkeys() {
    local pkey
    pkey="${1}"
    local tmp_home
    tmp_home=$(mktemp -d /tmp/gpg-XXXXXX)
    gpg --homedir "${tmp_home}" --batch --passphrase '' --yes --quick-gen-key "Secure Execution (secex) $buildid" rsa4096 encr none
    gpg --homedir "${tmp_home}" --armor --export secex > "${ignition_pubkey}"
    gpg --homedir "${tmp_home}" --armor --export-secret-key secex > "${pkey}"
    rm -rf "${tmp_home}"
}

# Other arch-specific bootloader changes
# shellcheck disable=SC2031
case "$arch" in
x86_64)
    # UEFI
    install_uefi
    if [ "${x86_bios_bootloader}" = 1 ]; then
        # And BIOS grub in addition.  See also
        # https://github.com/coreos/fedora-coreos-tracker/issues/32
        # Install BIOS/PReP bootloader using the target system's grub2-install,
        # see https://github.com/coreos/coreos-assembler/issues/3156
        chroot_run /sbin/grub2-install \
            --target i386-pc \
            --boot-directory $rootfs/boot \
            --modules mdraid1x \
            "$disk"
    fi
    ;;
aarch64)
    # Our aarch64 is UEFI only.
    install_uefi
    ;;
ppc64le)
    # to populate PReP Boot, i.e. support pseries
    chroot_run /sbin/grub2-install --target=powerpc-ieee1275 --boot-directory $rootfs/boot --no-nvram "${disk}${PREPPN}"
    install_grub_cfg
    ;;
s390x)
    ostree config --repo $rootfs/ostree/repo set sysroot.bootloader zipl
    rdcore_zipl_args=("--boot-mount=$rootfs/boot" "--append-karg=ignition.firstboot")
    # in the secex case, we run zipl at the end; in the non-secex case, we need
    # to run it now because zipl wants rw access to the bootfs
    if [[ ${secure_execution} -ne 1 ]]; then
        # in case builder itself runs with SecureExecution
        rdcore_zipl_args+=("--secex-mode=disable")
        chroot_run /usr/lib/dracut/modules.d/50rdcore/rdcore zipl "${rdcore_zipl_args[@]}"
    fi
    ;;
esac

# enable support for GRUB password
# shellcheck disable=SC2031
if [ "$arch" != s390x ]; then
    ostree config --repo $rootfs/ostree/repo set sysroot.bls-append-except-default 'grub_users=""'
fi

# For local secex build we create an empty file and later mount-bind real private key to it,
# so rdcore could append it to initrd. Best approach is to teach rdcore how to append file
# with different source and dest- paths.
if [[ ${secure_execution} -eq 1 ]] && [[ ! -e /dev/disk/by-id/virtio-genprotimg ]]; then
    touch "${deploy_root}/usr/lib/coreos/ignition.asc"
fi
touch $rootfs/boot/ignition.firstboot

fstrim -a -v
# Ensure the filesystem journals are flushed
for fs in $rootfs/boot $rootfs; do
    mount -o remount,ro "$fs"
    fsfreeze -f "$fs"
    fsfreeze -u "$fs"
done

umount -R $rootfs

create_dmverity() {
    local partlabel=$1; shift
    local mountpoint=$1; shift
    local datapart="/dev/disk/by-partlabel/${partlabel}"
    local hashpart="/dev/disk/by-partlabel/${partlabel}hash"
    # We have to use 512 here to match the filesystem sector size. It's less
    # efficient, but meh, it's for first boot only. Alternatively we could
    # change the filesystem sector size higher up.
    veritysetup format "${datapart}" "${hashpart}" \
        --root-hash-file "/tmp/${partlabel}-roothash" \
        --data-block-size 512
    veritysetup open "${datapart}" "${partlabel}" "${hashpart}" \
        --root-hash-file "/tmp/${partlabel}-roothash"
    mount -o ro "/dev/mapper/${partlabel}" "${mountpoint}"
}

# Save genprotimg input for later and don't run zipl here
rdcore_replacement() {
    local se_kargs_append se_initrd se_kernel se_parmfile
    local blsfile kernel initrd
    local se_script_dir se_tmp_disk se_tmp_mount se_tmp_boot

    se_kargs_append=("ignition.firstboot")
    while [ $# -gt 0 ]; do
        se_kargs_append+=("$1")
        shift
    done

    se_script_dir="/usr/lib/coreos-assembler/secex-genprotimgvm-scripts"
    se_tmp_disk=$(realpath /dev/disk/by-id/virtio-genprotimg)
    se_tmp_mount=$(mktemp -d /tmp/genprotimg-XXXXXX)
    se_tmp_boot="${se_tmp_mount}/genprotimg"
    mount "${se_tmp_disk}" "${se_tmp_mount}"
    mkdir "${se_tmp_boot}"

    se_initrd="${se_tmp_boot}/initrd.img"
    se_kernel="${se_tmp_boot}/vmlinuz"
    se_parmfile="${se_tmp_boot}/parmfile"

    # Ignition GPG private key
    mkdir -p "${se_tmp_boot}/usr/lib/coreos"
    generate_gpgkeys "${se_tmp_boot}/usr/lib/coreos/ignition.asc"

    blsfile=$(find "${rootfs}"/boot/loader/entries/*.conf)
    echo "$(grep options "${blsfile}" | cut -d' ' -f2-)" "${se_kargs_append[@]}" > "${se_parmfile}"
    kernel="${rootfs}/boot/$(grep linux "${blsfile}" | cut -d' ' -f2)"
    initrd="${rootfs}/boot/$(grep initrd "${blsfile}" | cut -d' ' -f2)"
    cp "${kernel}" "${se_kernel}"
    cp "${initrd}" "${se_initrd}"

    # genprotimg and zipl will be done outside this script
    # copy scripts for that step to the tmp disk
    cp "${se_script_dir}/genprotimg-script.sh" "${se_script_dir}/post-script.sh" "${se_tmp_mount}/"

    umount "${se_tmp_mount}"
    rmdir "${se_tmp_mount}"
}

if [[ ${secure_execution} -eq 1 ]]; then
    # set up dm-verity for the rootfs and bootfs
    create_dmverity root $rootfs
    create_dmverity boot $rootfs/boot
    # We need to run the genprotimg step in a separate step for rhcos release images
    if [ ! -e /dev/disk/by-id/virtio-genprotimg ]; then
        echo "Building local Secure Execution Image, running zipl and genprotimg"
        generate_gpgkeys "/tmp/ignition.asc"
        mount --rbind "/tmp/ignition.asc" "${deploy_root}/usr/lib/coreos/ignition.asc"
        # run zipl with root hashes as kargs
        rdcore_zipl_args+=("--secex-mode=enforce" "--hostkey=/dev/disk/by-id/virtio-hostkey")
        rdcore_zipl_args+=("--append-karg=rootfs.roothash=$(cat /tmp/root-roothash)")
        rdcore_zipl_args+=("--append-karg=bootfs.roothash=$(cat /tmp/boot-roothash)")
        rdcore_zipl_args+=("--append-file=/usr/lib/coreos/ignition.asc")
        chroot_run /usr/lib/dracut/modules.d/50rdcore/rdcore zipl "${rdcore_zipl_args[@]}"
    else
        echo "Building release Secure Execution Image, zipl and genprotimg will be run later"
        rdcore_replacement "rootfs.roothash=$(cat /tmp/root-roothash)" "bootfs.roothash=$(cat /tmp/boot-roothash)"
    fi

    # unmount and close everything
    umount -R $rootfs
    veritysetup close boot
    veritysetup close root
fi

rmdir $rootfs
