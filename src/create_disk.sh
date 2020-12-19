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

usage() {
    cat <<EOC
${0} create a supermin virtual machinge to create a
Fedora CoreOS style disk image from an OS Tree.

Options:
    --disk: disk device to use
    --help: show this helper
    --kargs: kernel CLI args
    --no-x86-bios-bootloader: don't install BIOS bootloader on x86_64

You probably don't want to run this script by hand. This script is
run as part of 'coreos-assembler build'.
EOC
}

config=
disk=
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
         *) echo "${flag} is not understood."; usage; exit 10;;
     esac;
done

udevtrig() {
    udevadm trigger
    udevadm settle
}

export PATH=$PATH:/sbin:/usr/sbin
arch="$(uname -m)"

disk=$(realpath /dev/disk/by-id/virtio-target)

config="${config:?--config must be defined}"

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

# First parse the old luks_rootfs flag (a custom "stringified bool")
if test "$(getconfig luks_rootfs)" = "yes"; then
    rootfs_type=luks
else
    rootfs_type=$(getconfig rootfs)
fi
case "${rootfs_type}" in
    xfs|ext4verity|luks|btrfs) ;;
    *) echo "Invalid rootfs type: ${rootfs_type}" 1>&2; exit 1;;
esac

bootfs=$(getconfig "bootfs")
grub_script=$(getconfig "grub-script")
ostree=$(getconfig "ostree-repo")
commit=$(getconfig "ostree-commit")
ref=$(getconfig "ostree-ref")
# We support not setting a remote name (used by RHCOS)
remote_name=$(getconfig_def "ostree-remote" "")
os_name=$(getconfig "osname")
rootfs_size=$(getconfig "rootfs-size")
buildid=$(getconfig "buildid")
imgid=$(getconfig "imgid")

set -x

# Partition and create fs's. The 0...4...a...1 uuid is a sentinal used by coreos-gpt-setup
# in ignition-dracut. It signals that the disk needs to have it's uuid randomized and the
# backup header moved to the end of the disk.
# Pin /boot and / to the partition number 3 and 4 respectively
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
        EFIPN=2
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n ${EFIPN}:0:+127M -c ${EFIPN}:EFI-SYSTEM -t ${EFIPN}:C12A7328-F81F-11D2-BA4B-00A0C93EC93B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
    s390x)
        # NB: in the bare metal case when targeting ECKD DASD disks, this
        # partition table is not what actually gets written to disk in the end:
        # coreos-installer has code which transforms it into a DASD-compatible
        # partition table and copies each partition individually bitwise.
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
    ppc64le)
        PREPPN=1
        # ppc64le doesn't use special uuid for root partition
        sgdisk -Z $disk \
        -U "${uninitialized_gpt_uuid}" \
        -n ${PREPPN}:0:+4M -c ${PREPPN}:PowerPC-PReP-boot -t ${PREPPN}:9E1A2D38-C612-4316-AA26-8B49521E5A8B \
        -n ${BOOTPN}:0:+384M -c ${BOOTPN}:boot \
        -n ${ROOTPN}:0:${rootfs_size} -c ${ROOTPN}:root -t ${ROOTPN}:0FC63DAF-8483-4772-8E79-3D69D8477DE4
        sgdisk -p "$disk"
        ;;
esac

udevtrig

root_dev="${disk}${ROOTPN}"
if [ "${rootfs_type}" = "luks" ]; then
    root_dev=/dev/mapper/crypt_root
    sgdisk -c ${ROOTPN}:luks_root "${disk}"

    touch tmp.key
    # Create the LUKS partition using the null_cipher and a sentinal
    # UUID similiar to the one used by coreos-gpt-setup. This is used
    # by ignition-dracut-reecrypt.  We use argon2i as it's the cryptsetup
    # default today, but explicitly specify just 512Mb in order to support
    # booting on smaller systems.
    cryptsetup luksFormat \
        -q \
        --type luks2 \
        --pbkdf argon2i \
        --pbkdf-memory 524288 \
        --label="crypt_rootfs" \
        --cipher=cipher_null \
        --key-file=tmp.key \
        --uuid='00000000-0000-4000-a000-000000000002' \
        "${disk}${ROOTPN}"

    # 'echo ""' acts as a test that you can use an empty
    # password. You can actually use _any_ string.
    echo "" | cryptsetup luksOpen \
        --allow-discards \
        "${disk}${ROOTPN}" crypt_root \
        --key-file=-

    udevtrig

    cryptsetup token import \
        "${disk}${ROOTPN}" \
        --token-id 9 \
        --key-slot=0 \
        <<<'{"type": "coreos", "keyslots": ["0"], "key": "", "ostree_ref": "'${ref}'"}'

    # This enabled discards, which is probably not a great idea for
    # those avoiding three-letter acronyms. For the vast majority of users
    # this is fine. See warning at:
    # https://gitlab.com/cryptsetup/cryptsetup/wikis/FrequentlyAskedQuestions
    extrakargs="${extrakargs} rd.luks.options=discard"
fi

bootargs=
case "${bootfs}" in
    ext4verity)
        # Need blocks to match host page size; TODO
        # really mkfs.ext4 should know this.
        bootargs="-b $(getconf PAGE_SIZE) -O verity"
        ;;
    ext4) ;;
    *) echo "Unhandled bootfs: ${bootfs}" 1>&2; exit 1 ;;
esac
mkfs.ext4 ${bootargs} "${disk}${BOOTPN}" -L boot -U "${bootfs_uuid}"
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
        mkfs.ext4 -b $(getconf PAGE_SIZE) -O verity -L root "${root_dev}" -U "${rootfs_uuid}"
        ;;
    btrfs)
        mkfs.btrfs -L root "${root_dev}" -U "${rootfs_uuid}"
        ;;
    xfs|luks|"")
        mkfs.xfs "${root_dev}" -L root -m reflink=1 -m uuid="${rootfs_uuid}"
        ;;
    *)
        echo "Unknown rootfs_type: $rootfs_type" 1>&2
        exit 1
        ;;
esac

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
mount "${disk}${BOOTPN}" $rootfs/boot
chcon $(matchpathcon -n /boot) $rootfs/boot
# FAT doesn't support SELinux labeling, it uses "genfscon", so we
# don't need to give it a label manually.
if [ ${EFIPN:+x} ]; then
    mkdir $rootfs/boot/efi
    mount "${disk}${EFIPN}" $rootfs/boot/efi
fi

# Now that we have the basic disk layout, initialize the basic
# OSTree layout, load in the ostree commit and deploy it.
ostree admin init-fs --modern $rootfs
if [ "${rootfs_type}" = "ext4verity" ]; then
    ostree config --repo=$rootfs/ostree/repo set ex-fsverity.required 'true'
fi
deploy_ref=
if test -n "${remote_name}"; then
    deploy_ref="${remote_name}:${ref}"
    time ostree pull-local --repo $rootfs/ostree/repo --remote="${remote_name}" "$ostree" "$ref"
else
    deploy_ref=$commit
    time ostree pull-local --repo $rootfs/ostree/repo "$ostree" "$commit"
fi
ostree admin os-init "$os_name" --sysroot $rootfs
# Note that $ignition_firstboot is interpreted by grub at boot time,
# *not* the shell here.  Hence the backslash escape.
allkargs="$extrakargs \$ignition_firstboot"
kargsargs=""
for karg in $allkargs
do
	kargsargs+="--karg-append=$karg "
done
ostree admin deploy "${deploy_ref}" --sysroot $rootfs --os "$os_name" $kargsargs

deploy_root="$rootfs/ostree/deploy/${os_name}/deploy/${commit}.0"
test -d "${deploy_root}"

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
  search --label boot --set prefix
fi
set prefix=($prefix)/grub2
configfile $prefix/grub.cfg
boot
EOF
    install_grub_cfg
}

# copy the grub config and any other files we might need
install_grub_cfg() {
    mkdir -p $rootfs/boot/grub2
    cp -v $grub_script $rootfs/boot/grub2/grub.cfg
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
    # current zipl expects 'title' to be first line, and no blank lines in BLS file
    # see https://github.com/ibm-s390-tools/s390-tools/issues/64
    blsfile=$(find $rootfs/boot/loader/entries/*.conf)
    tmpfile=$(mktemp)
    for f in title version linux initrd options; do
        echo $(grep $f $blsfile) >> $tmpfile
    done
    cat $tmpfile > $blsfile
    # we force firstboot in building base image on s390x, ignition-dracut hook will remove
    # this and update zipl for second boot
    # this is only a temporary solution until we are able to do firstboot check at bootloader
    # stage on s390x, either through zipl->grub2-emu or zipl standalone.
    # See https://github.com/coreos/ignition-dracut/issues/84
    # A similar hack is present in https://github.com/coreos/coreos-assembler/blob/master/src/gf-platformid#L55
    echo "$(grep options $blsfile) ignition.firstboot" > $tmpfile

    # ideally we want to invoke zipl with bls and zipl.conf but we might need
    # to chroot to $rootfs/ to do so. We would also do that when FCOS boot on its own.
    # without chroot we can use --target option in zipl but it requires kernel + initramfs
    # pair instead
    zipl --verbose \
        --target $rootfs/boot \
        --image $rootfs/boot/"$(grep linux $blsfile | cut -d' ' -f2)" \
        --ramdisk $rootfs/boot/"$(grep initrd $blsfile | cut -d' ' -f2)" \
        --parmfile $tmpfile
    ;;
esac

ostree config --repo $rootfs/ostree/repo set sysroot.bootloader "${bootloader_backend}"
# Opt-in to https://github.com/ostreedev/ostree/pull/1767 AKA
# https://github.com/ostreedev/ostree/issues/1265
ostree config --repo $rootfs/ostree/repo set sysroot.readonly true

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
