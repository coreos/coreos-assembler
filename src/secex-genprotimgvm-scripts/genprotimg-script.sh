#!bin/bash

set -exuo pipefail

echo "Preparing for genprotimg-daemon"

source="/build/genprotimg"
destination="/genprotimg"

# Files need to be named correctly
# genprotimg daemon can only see /genprotimg folder
cp "${source}/vmlinuz" "${source}/initrd.img" "${source}/parmfile" "${destination}/"

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
