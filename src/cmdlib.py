# Python version of cmdlib.sh

import hashlib
import json
import multiprocessing
import os
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime

libdir = '/usr/lib/coreos-assembler/'

def run_verbose(args, **kwargs):
    print("+ {}".format(subprocess.list2cmdline(args)))
    subprocess.check_call(args, **kwargs)


def write_json(path, data):
    dn = os.path.dirname(path)
    f = tempfile.NamedTemporaryFile(mode='w', dir=dn, delete=False)
    json.dump(data, f, indent=4)
    os.fchmod(f.file.fileno(), 0o644)
    os.rename(f.name, path)


def sha256sum_file(filename):
    h = hashlib.sha256()
    with open(filename, 'rb', buffering=0) as f:
        for b in iter(lambda: f.read(128 * 1024), b''):
            h.update(b)
    return h.hexdigest()


def fatal(msg):
    print('error: {}'.format(msg), file=sys.stderr)
    raise SystemExit(1)


def rfc3339_time(t=None):
    if t is None:
        t = datetime.utcnow()
    else:
        # if the need arises, we can convert to UTC, but let's just enforce
        # this doesn't slip by for now
        assert t.tzname() == 'UTC', "Timestamp must be in UTC format"
    return t.strftime("%Y-%m-%dT%H:%M:%SZ")


def rm_allow_noent(path):
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass


def run_supermin(workdir, script=''):
    vmpreparedir = f"{workdir}/tmp/supermin.prepare"
    if os.path.isdir(vmpreparedir):
        shutil.rmtree(vmpreparedir)
    os.makedirs(vmpreparedir, exist_ok=True)

    vmbuilddir = f"{workdir}/tmp/supermin.build"
    if os.path.isdir(vmbuilddir):
        shutil.rmtree(vmbuilddir)
    os.makedirs(vmbuilddir, exist_ok=True)

    rpms = []
    with open(f"{libdir}/vmdeps.txt") as vmdeps:
        for line in vmdeps:
            if not line or line.startswith('#'):
                continue
            for pkg in line.split():
                rpms.append(pkg)

    run_verbose(['supermin',
                 '--prepare',
                 '--use-installed',
                 '-o', f"{vmpreparedir}"] + rpms)

    with open(f"{libdir}/supermin-init-prelude.sh") as prelude:
        sm_init = prelude.read()

    with open(f"{workdir}/tmp/cmd.sh", "w+") as scriptfile:
        scriptfile.write(script)
    initscript = f'''#!/usr/bin/env bash
set -xeuo pipefail
workdir={workdir}
{sm_init}
RC=0
sh {workdir}/tmp/cmd.sh || RC=$?
echo $RC > {workdir}/tmp/rc
/sbin/fstrim -v {workdir}/cache
/sbin/poweroff -f
    '''
    with open(f"{vmpreparedir}/init", "w+") as vminit:
        vminit.write(initscript)
    os.chmod(f"{vmpreparedir}/init", 0o755)
    run_verbose(['tar',
                 '-C', f"{vmpreparedir}",
                 '-czf', f"{vmpreparedir}/init.tar.gz",
                 '--remove-files',
                 'init'])

    run_verbose(['supermin',
                 '--build', f"{vmpreparedir}",
                 '--size', '5G',
                 '-f', 'ext2',
                 '-o', f"{vmbuilddir}"])

    nproc = multiprocessing.cpu_count()
    run_verbose(['qemu-kvm', '-nodefaults', '-nographic', '-no-reboot',
                 '-smp', f"{nproc}",
                 '-m', '2048',
                 '-kernel', f"{vmbuilddir}/kernel",
                 '-initrd', f"{vmbuilddir}/initrd",
                 '-netdev', 'user,id=eth0,hostname=supermin',
                 '-device', 'virtio-net-pci,netdev=eth0',
                 '-device', 'virtio-scsi-pci,id=scsi0,bus=pci.0,addr=0x3',
                 '-drive', f"if=none,id=drive-scsi0-0-0-0,snapshot=on,file={vmbuilddir}/root",
                 '-device', 'scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=0,drive=drive-scsi0-0-0-0,id=scsi0-0-0-0,bootindex=1',
                 '-drive', f"if=none,id=drive-scsi0-0-0-1,discard=unmap,file={workdir}/cache/cache.qcow2",
                 '-device', 'scsi-hd,bus=scsi0.0,channel=0,scsi-id=0,lun=1,drive=drive-scsi0-0-0-1,id=scsi0-0-0-1',
                 '-virtfs', f"local,id=workdir,path={workdir},security_model=none,mount_tag=workdir",
                 '-serial', 'stdio', '-append', 'root=/dev/sda console=ttyS0 selinux=1 enforcing=0 autorelabel=1'])

    rc = 99
    with open(f"{workdir}/tmp/rc") as rcfile:
        rc = int(rcfile.readline())

    if rc != 0:
        raise Exception(f"failed to run supermin (rc: {rc})")
    return
