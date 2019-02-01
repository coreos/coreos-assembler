# Python version of cmdlib.sh
"""
Houses helper code for python based coreos-assembler commands.
"""

import hashlib
import json
import os
import subprocess
import sys
import tempfile

from datetime import datetime


def run_verbose(args, **kwargs):
    """
    Prints out the command being executed before executing a subprocess call.

    :param args: All non-keyword arguments
    :type args: list
    :param kwargs: All keyword arguments
    :type kwargs: dict
    :raises: CalledProcessError
    """
    print("+ {}".format(subprocess.list2cmdline(args)))

    # default to throwing exception
    if 'check' not in kwargs.keys():
        kwargs['check'] = True
    # capture_output is only on python 3.7+. Provide convenience here
    # until 3.7 is a baseline:
    if kwargs.pop('capture_output', False):
        kwargs['stdout'] = subprocess.PIPE
        kwargs['stderr'] = subprocess.PIPE

    try:
        process = subprocess.run(args, **kwargs)
    except subprocess.CalledProcessError:
        fatal("Error running command " + args[0])
    return process


def write_json(path, data):
    """
    Shortcut for writing a structure as json to the file system.

    :param path: The full path to the file to write
    :type: path: str
    :param data:  structure to write out as json
    :type data: dict or list
    :raises: ValueError, OSError
    """
    dn = os.path.dirname(path)
    f = tempfile.NamedTemporaryFile(mode='w', dir=dn, delete=False)
    json.dump(data, f, indent=4)
    os.fchmod(f.file.fileno(), 0o644)
    os.rename(f.name, path)


def load_json(path):
    """
    Shortcut for loading json from a file path.

    :param path: The full path to the file
    :type: path: str
    :returns: loaded json
    :rtype: dict
    :raises: IOError, ValueError
    """
    with open(path) as f:
        return json.load(f)

def sha256sum_file(path):
    """
    Calculates the sha256 sum from a path.

    :param path: The full path to the file
    :type: path: str
    :returns: The calculated sha256 sum
    :type: str
    """
    h = hashlib.sha256()
    with open(path, 'rb', buffering=0) as f:
        for b in iter(lambda: f.read(128 * 1024), b''):
            h.update(b)
    return h.hexdigest()


def fatal(msg):
    """
    Prints fatal error messages and exits execution.

    :param msg: The message to show to output
    :type msg: str
    :raises: SystemExit
    """
    print('fatal: {}'.format(msg), file=sys.stderr)
    raise SystemExit(1)

def info(msg):
    """
    Prints info messages.

    :param msg: The message to show to output
    :type msg: str
    """
    print('info: {}'.format(msg), file=sys.stderr)


def rfc3339_time(t=None):
    """
    Produces a rfc3339 compliant time string.

    :param t: The full path to the file
    :type: t: datetime.datetime
    :returns: a rfc3339 compliant time string
    :rtype: str
    """
    if t is None:
        t = datetime.utcnow()
    else:
        # if the need arises, we can convert to UTC, but let's just enforce
        # this doesn't slip by for now
        assert t.tzname() == 'UTC', "Timestamp must be in UTC format"
    return t.strftime("%Y-%m-%dT%H:%M:%SZ")


def rm_allow_noent(path):
    """
    Removes a file but doesn't error if the file does not exist.

    :param path: The full path to the file
    :type: path: str
    """
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
