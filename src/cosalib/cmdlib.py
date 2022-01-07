# Python version of cmdlib.sh
"""
Houses helper code for python based coreos-assembler commands.
"""
import glob
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import gi
import yaml

from botocore.exceptions import (
    ConnectionClosedError,
    ConnectTimeoutError,
    IncompleteReadError,
    ReadTimeoutError)

from flufl.lock import Lock

from tenacity import (
    stop_after_delay, stop_after_attempt, retry_if_exception_type)

gi.require_version("RpmOstree", "1.0")
from gi.repository import RpmOstree

from datetime import datetime, timezone

retry_stop = (stop_after_delay(10) | stop_after_attempt(5))
retry_boto_exception = (retry_if_exception_type(ConnectionClosedError) |
                      retry_if_exception_type(ConnectTimeoutError) |
                      retry_if_exception_type(IncompleteReadError) |
                      retry_if_exception_type(ReadTimeoutError))

THISDIR = os.path.dirname(os.path.abspath(__file__))


def retry_callback(retry_state):
    print(f"Retrying after {retry_state.outcome.exception()}")


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


def get_lock_path(path):
    """
    Return the lock path to use for a given path.
    """
    dn = os.path.dirname(path)
    bn = os.path.basename(path)
    return os.path.join(dn, f".{bn}.lock")


# Credit to @arithx
def merge_dicts(x, y):
    """
    Merge two dicts recursively, but based on the difference.
    """
    sd = set(x.keys()).symmetric_difference(y.keys())
    ret = {}
    for d in [x, y]:
        for k, v in d.items():
            if k in sd:
                # the key is only present in one dict, add it directly
                ret.update({k: v})
            elif type(x[k]) == dict and type(y[k]) == dict:
                # recursively merge
                ret.update({k: merge_dicts(x[k], y[k])})
            else:
                # first dictionary always takes precedence
                ret.update({k: x[k]})
    return ret


def write_json(path, data, lock_path=None, merge_func=None):
    """
    Shortcut for writing a structure as json to the file system.

    merge_func is a callable that takes two dict and merges them
    together.

    :param path: The full path to the file to write
    :type: path: str
    :param data:  structure to write out as json
    :type data: dict or list
    :param lock_path: path for the lock file to use
    :type lock_path: string
    :raises: ValueError, OSError
    """
    # lock before moving
    if not lock_path:
        lock_path = get_lock_path(path)

    with Lock(lock_path):
        if callable(merge_func):
            try:
                disk_data = load_json(path, require_exclusive=False)
            except FileNotFoundError:
                disk_data = {}
            mem_data = data.copy()
            data = merge_func(disk_data, mem_data)

        # we could probably write directly to the file,
        # but set the permissions to RO
        dn = os.path.dirname(path)
        f = tempfile.NamedTemporaryFile(mode='w', dir=dn, delete=False)
        json.dump(data, f, indent=4)
        os.fchmod(f.file.fileno(), 0o644)
        shutil.move(f.name, path)


def load_json(path, require_exclusive=True, lock_path=None):
    """
    Shortcut for loading json from a file path.

    :param path: The full path to the file
    :type: path: str
    :param require_exclusive: lock file for exclusive read
    :type require_exclusive: bool
    :param lock_path: path for the lock file to use
    :type lock_path: string
    :returns: loaded json
    :rtype: dict
    :raises: IOError, ValueError
    """
    lock = None
    if require_exclusive:
        if not lock_path:
            lock_path = get_lock_path(path)
        lock = Lock(lock_path)
        lock.lock()
    try:
        with open(path) as f:
            return json.load(f)
    finally:
        if lock:
            lock.unlock(unconditionally=True)


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
    raise SystemExit(msg)


def info(msg):
    """
    Prints info messages.

    :param msg: The message to show to output
    :type msg: str
    """
    sys.stderr.write(f"info: {msg}")


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


# In coreos-assembler, we are strongly oriented towards the concept of a single
# versioned "build" object that has artifacts.  But rpm-ostree (among other things)
# really natively wants to operate on unpacked ostree repositories.  So, we maintain
# a `tmp/repo` (along with `cache/repo-build`) that are treated as caches.
# In some cases, such as building a qemu image, then later trying to generate
# a metal image, we may not have preserved that cache.
#
# Call this function to ensure that the ostree commit for a given build is in tmp/repo.
def import_ostree_commit(repo, buildpath, buildmeta, force=False):
    commit = buildmeta['ostree-commit']
    tarfile = os.path.join(buildpath, buildmeta['images']['ostree']['path'])
    # create repo in case e.g. tmp/ was cleared out; idempotent
    subprocess.check_call(['ostree', 'init', '--repo', repo, '--mode=archive'])

    # in the common case where we're operating on a recent build, the OSTree
    # commit should already be in the tmprepo
    commitpartial = os.path.join(repo, f'state/{commit}.commitpartial')
    if (subprocess.call(['ostree', 'show', '--repo', repo, commit],
                        stdout=subprocess.DEVNULL,
                        stderr=subprocess.DEVNULL) == 0
            and not os.path.isfile(commitpartial)
            and not force):
        return

    print(f"Extracting {commit}")
    # extract in a new tmpdir inside the repo itself so we can still hardlink
    if tarfile.endswith('.tar'):
        with tempfile.TemporaryDirectory(dir=repo) as d:
            subprocess.check_call(['tar', '-C', d, '-xf', tarfile])
            subprocess.check_call(['ostree', 'pull-local', '--repo', repo,
                                   d, commit])
    elif tarfile.endswith('.ociarchive'):
        # We do this in two stages, because right now ex-container only writes to
        # non-archive repos.  Also, in the privileged case we need sudo to write
        # to `repo-build`, though it might be good to change this by default.
        if os.environ.get('COSA_PRIVILEGED', '') == '1':
            build_repo = os.path.join(repo, '../../cache/repo-build')
            subprocess.check_call(['sudo', 'ostree', 'container', 'import', '--repo', build_repo,
                                   '--write-ref', buildmeta['buildid'], 'ostree-unverified-image:oci-archive:' + tarfile])
            subprocess.check_call(['sudo', 'ostree', f'--repo={repo}', 'pull-local', build_repo, buildmeta['buildid']])
            uid = os.getuid()
            gid = os.getgid()
            subprocess.check_call(['sudo', 'chown', '-hR', f"{uid}:{gid}", repo])
        else:
            with tempfile.TemporaryDirectory() as tmpd:
                subprocess.check_call(['ostree', 'init', '--repo', tmpd, '--mode=bare-user'])
                subprocess.check_call(['ostree', 'container', 'import', '--repo', tmpd,
                                       '--write-ref', buildmeta['buildid'], 'ostree-unverified-image:oci-archive:' + tarfile])
                subprocess.check_call(['ostree', f'--repo={repo}', 'pull-local', tmpd, buildmeta['buildid']])


def get_basearch():
    try:
        return get_basearch.saved
    except AttributeError:
        get_basearch.saved = RpmOstree.get_basearch()
        return get_basearch.saved


def parse_date_string(date_string):
    """
    Parses the date strings expected from the build system. Returned
    datetime instances will be in utc.
    :param date_string: string to turn into date. Format: %Y-%m-%dT%H:%M:%SZ
    :type date_string: str
    :returns: datetime instance from the date string
    :rtype: datetime.datetime
    :raises: ValueError, TypeError
    """
    dt = datetime.strptime(date_string, '%Y-%m-%dT%H:%M:%SZ')
    return dt.replace(tzinfo=timezone.utc)


def get_timestamp(entry):

    # ignore dirs missing meta.json
    meta_file = os.path.join(entry.path, 'meta.json')
    if not os.path.isfile(meta_file):
        print(f"Ignoring directory {entry.name}")
        return None

    # collect dirs and timestamps
    j = load_json(meta_file)

    # Older versions only had ostree-timestamp
    ts = j.get('coreos-assembler.build-timestamp') or j['ostree-timestamp']
    return parse_date_string(ts)


def image_info(image):
    try:
        out = json.loads(run_verbose(
            ['qemu-img', 'info', '--output=json', image],
            capture_output=True).stdout
        )

        # Fixed VPC/VHD v1 disks are really raw images with a VHD footer.
        # The VHD footer uses 'conectix' as the identify in first 8 bytes
        # of the last 512 bytes. Sadly, 'qemu-img' does not identify it
        # properly.
        if out.get("format") == "raw":
            with open(image, 'rb') as imgf:
                imgf.seek(-512, os.SEEK_END)
                data = imgf.read(8)
                if data == b"conectix":
                    out['format'] = "vpc"
                    out['submformat'] = "fixed"
        return out
    except Exception as e:
        raise Exception(f"failed to inspect {image} with qemu", e)


# Hackily run some bash code from cmdlib.sh helpers.
def cmdlib_sh(script):
    subprocess.check_call(['bash', '-c', f'''
        set -euo pipefail
        source {THISDIR}/../cmdlib.sh
        {script}
    '''])


def flatten_image_yaml_to_file(srcfile, outfile):
    flattened = flatten_image_yaml(srcfile)
    with open(outfile, 'w') as f:
        yaml.dump(flattened, f)


def merge_lists(x, y, k):
    x[k] = x.get(k, [])
    assert type(x[k]) == list
    y[k] = y.get(k, [])
    assert type(y[k]) == list
    x[k].extend(y[k])


def flatten_image_yaml(srcfile, base=None):
    if base is None:
        base = {}

    with open(srcfile) as f:
        srcyaml = yaml.safe_load(f)

    # first, special-case list values
    merge_lists(base, srcyaml, 'extra-kargs')
    merge_lists(base, srcyaml, 'ignition-network-kcmdline')

    # then handle all the non-list values
    base = merge_dicts(base, srcyaml)

    if 'include' not in srcyaml:
        return base

    fn = os.path.join(os.path.dirname(srcfile), srcyaml['include'])
    del base['include']
    return flatten_image_yaml(fn, base)


def ensure_glob(pathname, **kwargs):
    '''Call glob.glob(), and fail if there are no results.'''
    ret = glob.glob(pathname, **kwargs)
    if not ret:
        raise Exception(f'No matches for {pathname}')
    return ret
