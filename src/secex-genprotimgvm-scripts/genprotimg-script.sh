#!bin/bash
set -exuo pipefail

echo "Preparing for genprotimg-daemon"

# CoreOS based secure-vm uses '/var' as prefix
PREFIX=/
if [ -e /var/genprotimg ]; then
    PREFIX=/var
fi

destination="${PREFIX}/genprotimg"
parmfile="${PREFIX}/build/parmfile"

# This is our coreos.qemu-secex.qcow2
disk=$(realpath /dev/disk/by-id/virtio-target)


# 'boot' labeled partition on target image
disk_boot="${disk}3"
boot_mnt=$(mktemp -d /tmp/boot-XXXXXX)
mount -o ro "${disk_boot}" "${boot_mnt}"
blsfile=$(ls "${boot_mnt}"/loader/entries/*.conf)
kernel=$(grep linux "${blsfile}" | cut -d' ' -f2)
initrd=$(grep initrd "${blsfile}" | cut -d' ' -f2)
# Files need to be named correctly
cp "${boot_mnt}/${kernel}" "${destination}/vmlinuz"
cp "${boot_mnt}/${initrd}" "${destination}/initrd.img"
# Generate full cmdline
echo "$(grep options "${blsfile}" | cut -d' ' -f2-) $(<${parmfile})" > "${destination}/parmfile"
umount "${boot_mnt}"
rmdir "${boot_mnt}"


# We pass Ignition gpg private key from COSA to the VM as virtual disk
gpg_private_key=$(realpath /dev/disk/by-id/virtio-gpgkey)
gpg_dir=$(mktemp -d)
pkey="usr/lib/coreos/ignition.asc"
mkdir -p "${gpg_dir}/usr/lib/coreos"
cat "${gpg_private_key}" > "${gpg_dir}/${pkey}"
# Append Ignition GPG private key to initramfs
echo "${pkey}" | cpio -D "${gpg_dir}" --quiet -H newc -o | gzip -9 -n >> "${destination}/initrd.img"
rm -rf "${gpg_dir}"


# Signal daemon that it can run genprotimg
touch "${destination}/signal.file"

# Wait for genprotimg execution
while [ -e "$destination/signal.file" ] && [ ! -e "$destination/error" ]; do
    sleep 1
done
if [ -e "$destination/error" ] || [ ! -e "${destination}/se.img" ]; then
    ls -lha $destination
    echo "Failed to run genprotimg"
    exit 1
fi
