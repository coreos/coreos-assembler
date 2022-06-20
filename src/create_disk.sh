#!/bin/sh
# This script is run in supermin to create a Fedora CoreOS style
# disk image, very much in the spirit of the original
# Container Linux (orig CoreOS) disk layout, although adapted
# for OSTree, and using XFS for /, among other things.
# Some more background in https://github.com/coreos/fedora-coreos-tracker/issues/18
# The layout is intentionally not very configurable at this time,
# although see also https://github.com/coreos/coreos-assembler/pull/298
# For people building "derived"/custom FCOS-like systems, feel free to file
# an issue and we can discuss configuration needs.
set -euo pipefail

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
    --disk: disk device to use
    --help: show this help
    --kargs: kernel CLI args
    --platform: Ignition platform ID
    --platforms-json: platforms.yaml in JSON format
    --no-x86-bios-bootloader: don't install BIOS bootloader on x86_64
    --with-secure-execution:  enable IBM SecureExecution

You probably don't want to run this script by hand. This script is
run as part of 'coreos-assembler build'.
EOC
}

propagate_luks_config() {
    local key="$1"
    local lbl="crypt_${1}fs"
    # Moving key file to final destination
    mkdir -m 700 -p $deploy_root/etc/luks
    mv /tmp/${key}-luks-key $deploy_root/etc/luks/$key
    chmod 0400 $deploy_root/etc/luks/$key
    local uuid=$(cryptsetup luksUUID /dev/disk/by-label/$lbl)
    if [[ ! -e $deploy_root/etc/crypttab ]]; then
        touch $deploy_root/etc/crypttab
        chmod 0600 $deploy_root/etc/crypttab
    fi
    echo "$lbl UUID=${uuid} /etc/luks/$key luks" >> $deploy_root/etc/crypttab
}

create_luks_partition() {
    local key="/tmp/${1}-luks-key"
    local lbl="crypt_${1}fs"
    local dev="$2"
    # Generating random key
    dd if=/dev/urandom of=$key bs=1024 count=4
    chmod 0400 $key
    cryptsetup luksFormat -q \
                        --type luks2 \
                        --label="$lbl" \
                        --key-file=$key \
                        $dev

    cryptsetup luksOpen $dev \
                        $lbl \
                        --key-file=$key

}

config=
disk=
platform=metal
platforms_json=
secure_execution=0
x86_bios_bootloader=1
extrakargs=""

while [ $# -gt 0 ];
do
    flag="${1}"; shift;
    case "${flag}" in
        --config)                config="${1}"; shift;;
        --disk)                  disk="${1}"; shift;;
        --help)                  usage; exit;;
        --kargs)                 extrakargs="${extrakargs} ${1}"; shift;;
        --no-x86-bios-bootloader) x86_bios_bootloader=0;;
        --platform)              platform="${1}"; shift;;
        --platforms-json)        platforms_json="${1}"; shift;;
        --with-secure-execution) secure_execution=1;;
         *) echo "${flag} is not understood."; usage; exit 10;;
     esac;
done

udevtrig() {
    udevadm trigger
    udevadm settle
}

export PATH=$PATH:/sbin:/usr/sbin
arch="$(uname -m)"

if [ -n "$platforms_json" ]; then
    platform_grub_cmds=$(jq -r ".${arch}.${platform}.grub_commands // [] | join(\"\\\\n\")" < "${platforms_json}")
    platform_kargs=$(jq -r ".${arch}.${platform}.kernel_arguments // [] | join(\" \")" < "${platforms_json}")
else
    # Add legacy kargs and console settings
    platform_grub_cmds='serial --speed=115200\nterminal_input serial console\nterminal_output serial console'
    DEFAULT_TERMINAL=$(. $(dirname "$0")/cmdlib.sh; echo $DEFAULT_TERMINAL)
    # On each s390x hypervisor, a tty would be automatically detected by the
    # kernel and systemd, there is no need to specify one.  However, we keep
    # DEFAULT_TERMINAL as ttysclp0, which is helpful for building/testing
    # with KVM+virtio (cmd-run).  For aarch64, ttyAMA0 is used as the
    # default console
    case "$arch" in
        "aarch64"|"s390x") platform_kargs= ;;
        *) platform_kargs="console=tty0 console=${DEFAULT_TERMINAL},115200n8" ;;
    esac
fi
if [ -n "${platform_kargs}" ]; then
    extrakargs="${extrakargs} ${platform_kargs}"
fi

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
    jq -re .'"'$k'"' < ${config}
}
# Return a configuration value, or default if not set
getconfig_def() {
    k=$1
    shift
    default=$1
    shift
    jq -re .'"'$k'"'//'"'${default}'"' < ${config}
}

rootfs_type=$(getconfig rootfs)
case "${rootfs_type}" in
    xfs|ext4verity|btrfs) ;;
    *) echo "Invalid rootfs type: ${rootfs_type}" 1>&2; exit 1;;
esac
rootfs_args=$(getconfig_def "rootfs-args" "")

bootfs=$(getconfig "bootfs")
grub_script=$(getconfig "grub-script")
ostree_container=$(getconfig "ostree-container")
commit=$(getconfig "ostree-commit")
ref=$(getconfig "ostree-ref")
# We support not setting a remote name (used by RHCOS)
remote_name=$(getconfig_def "ostree-remote" "")
deploy_via_container=$(getconfig "deploy-via-container" "")
container_imgref=$(getconfig "container-imgref" "")
os_name=$(getconfig "osname")
rootfs_size=$(getconfig "rootfs-size")
buildid=$(getconfig "buildid")
imgid=$(getconfig "imgid")
bootfs_metadata_csum_seed=$(getconfig_def "bootfs_metadata_csum_seed" "false")

set -x

# Partition and create fs's. The 0...4...a...1 uuid is a sentinal used by coreos-gpt-setup
# in ignition-dracut. It signals that the disk needs to have it's uuid randomized and the
# backup header moved to the end of the disk.
# Pin /se, /boot and / to the partition number 1, 3 and 4 respectively. Also insert reserved
# partitions on aarch64/ppc64le to keep the 1,2,3,4 partition numbers aligned across
# x86_64/aarch64/ppc64le. We decided not to try to achieve partition parity on s390x
# because a bare metal install onto an s390x DASD translates the GPT to DASD partitions
# and we only get three of those. https://github.com/coreos/fedora-coreos-tracker/issues/855
SDPART=1
BOOTPN=3
ROOTPN=4
# Make the size relative
if [ "${rootfs_size}" != "0" ]; then
    rootfs_size="+${rootfs_size}"
fi
case "$arch" in
    x86_64)
        EFIPN=2
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n 1:0:+1M -c 1:BIOS-BOOT -t 1:21686148-6449-6E6F-744E-656564454649 \
        -n ${EFIPN}:0:+127M -c ${EFIPN}:EFI-SYSTEM -t ${EFIPN}:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
    aarch64)
        RESERVEDPN=1
        EFIPN=2
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n ${RESERVEDPN}:0:+1M -c ${RESERVEDPN}:reserved -t ${RESERVEDPN}:8DA63339-0007-60C0-C436-083AC8230908 \
        -n ${EFIPN}:0:+127M -c ${EFIPN}:EFI-SYSTEM -t ${EFIPN}:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
    s390x)
        if [[ ${secure_execution} -eq 1 ]]; then
            sgdisk -Z $disk \
                -U "${uninitialized_gpt_uuid}" \
                -n ${SDPART}:0:+200M -c ${SDPART}:se -t ${SDPART}:0FC63DAF-8483-4772-8E79-3D69D8477DE4 \
                -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
                -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        else
            # NB: in the bare metal case when targeting ECKD DASD disks, this
            # partition table is not what actually gets written to disk in the end:
            # coreos-installer has code which transforms it into a DASD-compatible
            # partition table and copies each partition individually bitwise.
            sgdisk -Z $disk \
                -U "${uninitialized_gpt_uuid}" \
                -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
                -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        fi
        sgdisk -p "$disk"
        ;;
    ppc64le)
        PREPPN=1
        RESERVEDPN=2
        # ppc64le doesn't use special uuid for root partition
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n ${PREPPN}:0:+4M -c ${PREPPN}:PowerPC-PReP-boot -t ${PREPPN}:9E1A2D38-C612-4316-AA26-8B49521E5A8B \
        -n ${RESERVEDPN}:0:+1M -c ${RESERVEDPN}:reserved -t ${RESERVEDPN}:8DA63339-0007-60C0-C436-083AC8230908 \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
esac

udevtrig

zipl_dev="${disk}${SDPART}"
boot_dev="${disk}${BOOTPN}"
root_dev="${disk}${ROOTPN}"

bootargs=
# If the bootfs_metadata_csum_seed image.yaml knob is set to true then
# we'll enable the metadata_csum_seed filesystem feature. This is
# gated behind an image.yaml knob because support for this feature
# flag was only recently added to grub.
# https://lists.gnu.org/archive/html/grub-devel/2021-06/msg00031.html
if [ "${bootfs_metadata_csum_seed}" == "true" ]; then
    bootargs="-O metadata_csum_seed"
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
    mkfs.ext4 ${bootargs} "${zipl_dev}" -L se -U random
    # /boot must be encrypted
    create_luks_partition boot ${boot_dev}
    # / must be encrypted
    create_luks_partition root ${root_dev}
    # reset to devmapper devices
    boot_dev="/dev/mapper/crypt_bootfs"
    root_dev="/dev/mapper/crypt_rootfs"
fi

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
        mkfs.ext4 -b $(getconf PAGE_SIZE) -O verity -L root "${root_dev}" -U "${rootfs_uuid}" ${rootfs_args}
        ;;
    btrfs)
        mkfs.btrfs -L root "${root_dev}" -U "${rootfs_uuid}" ${rootfs_args}
        ;;
    xfs|"")
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
chcon $(matchpathcon -n /) ${rootfs}
mkdir ${rootfs}/boot
chcon $(matchpathcon -n /boot) $rootfs/boot
mount "${boot_dev}" $rootfs/boot
chcon $(matchpathcon -n /boot) $rootfs/boot
# FAT doesn't support SELinux labeling, it uses "genfscon", so we
# don't need to give it a label manually.
if [ ${EFIPN:+x} ]; then
    mkdir $rootfs/boot/efi
    mount "${disk}${EFIPN}" $rootfs/boot/efi
fi
if [[ ${secure_execution} -eq 1 ]]; then
    mkdir ${rootfs}/se
    chcon $(matchpathcon -n /boot) $rootfs/se
fi

# Now that we have the basic disk layout, initialize the basic
# OSTree layout, load in the ostree commit and deploy it.
ostree admin init-fs --modern $rootfs
# Initialize the "stateroot"
ostree admin os-init "$os_name" --sysroot $rootfs

# Propagate flags into target repository
if [ "${rootfs_type}" = "ext4verity" ]; then
    ostree config --repo=$rootfs/ostree/repo set ex-fsverity.required 'true'
fi

# Compute kargs
allkargs="$extrakargs"
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
    ostree container image deploy --imgref "${ostree_container}" \
        ${container_imgref:+--target-imgref $container_imgref} \
        --write-commitid-to /tmp/commit.txt \
        --stateroot "$os_name" --sysroot $rootfs $kargsargs
    deploy_commit=$(cat /tmp/commit.txt)
    rm /tmp/commit.txt
else
    # Pull the commit
    time ostree container unencapsulate --repo=$rootfs/ostree/repo "${ostree_container}"
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

# we use pure BLS on most architectures; this may
# be overridden below
bootloader_backend=none

install_uefi() {
    # https://github.com/coreos/fedora-coreos-tracker/issues/510
    # See also https://github.com/ostreedev/ostree/pull/1873#issuecomment-524439883
    /usr/bin/bootupctl backend install --src-root="${deploy_root}" "${rootfs}"
    # We have a "static" grub config file that basically configures grub to look
    # in the RAID called "md-boot", if it exists, or the partition labeled "boot".
    local target_efi="$rootfs/boot/efi"
    local grubefi=$(find "${target_efi}/EFI/" -maxdepth 1 -type d | grep -v BOOT)
    local vendor_id="${grubefi##*/}"
    local vendordir="${target_efi}/EFI/${vendor_id}"
    mkdir -p "${vendordir}"
	cat > ${vendordir}/grub.cfg << 'EOF'
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
    mkdir -p -m 0700 $rootfs/boot/grub2
    printf "%s\n" "$grub_script" | \
        sed -E 's@(^# CONSOLE-SETTINGS-START$)@\1'"${platform_grub_cmds:+\\n${platform_grub_cmds}}"'@' \
        > $rootfs/boot/grub2/grub.cfg
    if [ -n "$platforms_json" ]; then
        # Copy platforms table if it's non-empty for this arch
        if jq -e ".$arch" < "$platforms_json" > /dev/null; then
            mkdir -p "$rootfs/boot/coreos"
            jq ".$arch" < "$platforms_json" > "$rootfs/boot/coreos/platforms.json"
        fi
    fi
}

# Other arch-specific bootloader changes
case "$arch" in
x86_64)
    # UEFI
    install_uefi
    if [ "${x86_bios_bootloader}" = 1 ]; then
        # And BIOS grub in addition.  See also
        # https://github.com/coreos/fedora-coreos-tracker/issues/32
        grub2-install \
            --target i386-pc \
            --boot-directory $rootfs/boot \
            --modules mdraid1x \
            $disk
    fi
    ;;
aarch64)
    # Our aarch64 is UEFI only.
    install_uefi
    ;;
ppc64le)
    # to populate PReP Boot, i.e. support pseries
    grub2-install --target=powerpc-ieee1275 --boot-directory $rootfs/boot --no-nvram "${disk}${PREPPN}"
    install_grub_cfg
    ;;
s390x)
    bootloader_backend=zipl
    rdcore_args=("--boot-mount=$rootfs/boot" "--kargs=ignition.firstboot")
    if [[ ${secure_execution} -eq 1 ]]; then
        rdcore_args+=("--secex-mode=enforce" "--rootfs=$deploy_root" "--hostkey=/dev/disk/by-id/virtio-hostkey")
        propagate_luks_config boot
        propagate_luks_config root
    else
        # in case builder itself runs with SecureExecution
        rdcore_args+=("--secex-mode=disable")
    fi
    # chroot, cause userspace may be not same as the supermin environment
    for mnt in dev proc sys run var tmp; do
        mount --rbind "/$mnt" "${deploy_root}/$mnt"
    done
    chroot ${deploy_root} /usr/lib/dracut/modules.d/50rdcore/rdcore zipl ${rdcore_args[@]}
    ;;
esac

ostree config --repo $rootfs/ostree/repo set sysroot.bootloader "${bootloader_backend}"
# Opt-in to https://github.com/ostreedev/ostree/pull/1767 AKA
# https://github.com/ostreedev/ostree/issues/1265
ostree config --repo $rootfs/ostree/repo set sysroot.readonly true
# enable support for GRUB password
if [ "${bootloader_backend}" = "none" ]; then
    ostree config --repo $rootfs/ostree/repo set sysroot.bls-append-except-default 'grub_users=""'
fi

touch $rootfs/boot/ignition.firstboot

# Finally, add the immutable bit to the physical root; we don't
# expect people to be creating anything there.  A use case for
# OSTree in general is to support installing *inside* the existing
# root of a deployed OS, so OSTree doesn't do this by default, but
# we have no reason not to enable it here.  Administrators should
# generally expect that state data is in /etc and /var; if anything
# else is in /sysroot it's probably by accident.
chattr +i $rootfs

fstrim -a -v
# Ensure the filesystem journals are flushed
for fs in $rootfs/boot $rootfs; do
    mount -o remount,ro $fs
    xfs_freeze -f $fs
done
umount -R $rootfs

rmdir $rootfs
