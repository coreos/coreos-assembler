#!/bin/bash

set -exuo pipefail

echo "Moving sdboot and executing zipl"

workdir="/build"
sdboot="/genprotimg/se.img"
genprotimg_dir="${workdir}/genprotimg"
se_boot=$(mktemp -d /tmp/se-XXXXXX)

disk=$(realpath /dev/disk/by-id/virtio-target)
disk_se="${disk}1"

mount "${disk_se}" "${se_boot}"
cp "${sdboot}" "${se_boot}/sdboot"
zipl -V -i ${se_boot}/sdboot -t ${se_boot}

# Disable debug output, the last message should be success
set +x
echo "Success, added sdboot to image and executed zipl"

umount "${se_boot}"
rm -rf "${se_boot}"
