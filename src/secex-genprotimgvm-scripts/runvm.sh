#!/bin/bash

set -euo pipefail

vmrundir="${workdir}/tmp/build.secex"
memory_default=2048
runvm_console="${vmrundir}/runvm-console.txt"
genprotimgvm=
qemu_args=()
while true; do
    case "$1" in
    --genprotimgvm)
        genprotimgvm="$2"
        shift
        ;;
    --)
        shift
        break
        ;;
    -*)
        fatal "$0: unrecognized option: $1"
        exit 1
        ;;
    *)
        break
        ;;
    esac
    shift
done
if [ -z "${genprotimgvm}" ]; then
    echo "Missing option --genprotimgvm"
fi
while [ $# -gt 0 ]; do
    qemu_args+=("$1")
    shift
done

set -x

[[ -d "${vmrundir}" ]] && rm -rf "${vmrundir}"
mkdir "${vmrundir}"
touch "${runvm_console}"

kola_args=(kola qemuexec -m "${COSA_SUPERMIN_MEMORY:-${memory_default}}" --auto-cpus -U --workdir none \
       --console-to-file "${runvm_console}")

base_qemu_args=(-drive "if=none,id=buildvm,format=qcow2,snapshot=on,file=${genprotimgvm},index=1" -device virtio-blk-ccw,drive=buildvm,bootindex=1 \
        -no-reboot -nodefaults -device virtio-serial \
        -device virtserialport,chardev=virtioserial0,name=cosa-cmdout -chardev stdio,id=virtioserial0
           )

if [ -z "${GENPROTIMGVM_SE_OFF:-}" ]; then
    base_qemu_args+=(-object s390-pv-guest,id=pv0 -machine confidential-guest-support=pv0)
else
    echo "No secure execution enabled for build-VM, happy debugging"
fi

if ! "${kola_args[@]}" -- "${base_qemu_args[@]}" \
    "${qemu_args[@]}" <&-; then # the <&- here closes stdin otherwise qemu waits forever
    cat "${runvm_console}"
    echo "Failed to run 'kola qemuexec'"
    exit 1
fi

cat "${runvm_console}"

if ! grep -q "Success, added sdboot to image and executed zipl" "${runvm_console}"; then
   echo "Could not find success message, genprotimg failed."
   exit 1
fi

exit 0
