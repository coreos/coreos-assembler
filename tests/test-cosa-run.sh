#!/bin/bash
# This verifies e.g. `cosa run`.
set -xeuo pipefail
tmpdir=$(mktemp -d -p /var/tmp)
cd ${tmpdir}
coreos-installer download -a s390x -p qemu -f qcow2.xz --decompress
cosa run --arch s390x *.qcow2 -x "cat /proc/cpuinfo" > cpuinfo.txt
grep -F 'IBM/S390' cpuinfo.txt
cd -
rm "${tmpdir}" -rf
echo "ok cosa run full emulation"
