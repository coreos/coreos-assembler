# Python version of cmdlib.sh

import hashlib
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime


def run_verbose(args, **kwargs):
    print("+ {}".format(subprocess.list2cmdline(args)))
    subprocess.check_call(args, **kwargs)


def write_json(path, data):
    dn = os.path.dirname(path)
    f = tempfile.NamedTemporaryFile(mode='w', dir=dn, delete=False)
    json.dump(data, f)
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
    return t.strftime("%Y-%m-%dT%H:%M:%SZ")
