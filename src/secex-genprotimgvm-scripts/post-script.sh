#!/bin/bash

set -exuo pipefail

echo "Moving sdboot and executing zipl"

# CoreOS based secure-vm uses '/var' as prefix
PREFIX=/
if [ -e /var/genprotimg ]; then
    PREFIX=/var
fi

# This is our coreos.qemu-secex.qcow2
disk=$(realpath /dev/disk/by-id/virtio-target)


# 'se' labeled partition on target image, holds 'sdboot' image
disk_se="${disk}1"
se_mnt=$(mktemp -d /tmp/se-XXXXXX)
mount "${disk_se}" "${se_mnt}"
cp "${PREFIX}/genprotimg/se.img" "${se_mnt}/sdboot"
zipl -V -i "${se_mnt}/sdboot" -t "${se_mnt}"
umount "${se_mnt}"
rmdir "${se_mnt}"


# Disable debug output, the last message should be success
set +x
echo "Success, added sdboot to image and executed zipl"
