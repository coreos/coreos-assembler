#!bin/bash

set -exuo pipefail

echo "Preparing for genprotimg-daemon"

source="/build/genprotimg"
destination="/genprotimg"
pkey="usr/lib/coreos/ignition.asc"

trap "rm -f ${source}/${pkey}" EXIT

# Files need to be named correctly
# genprotimg daemon can only see /genprotimg folder
cp "${source}/vmlinuz" "${source}/initrd.img" "${source}/parmfile" "${destination}/"

# Append Ignition GPG private key to initramfs
cd "${source}"
echo "${pkey}" | cpio --quiet -H newc -o | gzip -9 -n >> "${destination}/initrd.img"
rm "${pkey}"

# Signal daemon that it can run genprotimg
touch "${destination}/signal.file"

# Wait for genprotimg execution
while [ -e "$destination/signal.file" ] && [ ! -e "$destination/error" ]; do
    sleep 5
done
if [ -e "$destination/error" ] || [ ! -e "${destination}/se.img" ]; then
    ls -lha $destination
    echo "Failed to run genprotimg"
    exit 1
fi
