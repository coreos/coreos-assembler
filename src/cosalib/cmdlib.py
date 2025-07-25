# Python version of cmdlib.sh
"""
Houses helper code for python based coreos-assembler commands.
"""
import glob
import hashlib
import json
import logging
import os
import re
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
    ReadTimeoutError,
    EndpointConnectionError)

from flufl.lock import Lock

from tenacity import (
    stop_after_delay, stop_after_attempt, retry_if_exception_type, wait_exponential)

gi.require_version("RpmOstree", "1.0")
from gi.repository import RpmOstree

import datetime

# Set up logging
logging.basicConfig(level=logging.INFO,
                    format="%(asctime)s %(levelname)s - %(message)s")

# there's no way to say "forever", so just use a huge number
LOCK_DEFAULT_LIFETIME = datetime.timedelta(weeks=52)

retry_stop = (stop_after_delay(10) | stop_after_attempt(5))
# for operations that want to be more persistent
retry_stop_long = (stop_after_delay(60 * 5))  # 5 minutes
retry_wait_long = (wait_exponential(max=10))
retry_boto_exception = (retry_if_exception_type(ConnectionClosedError) |
                      retry_if_exception_type(ConnectTimeoutError) |
                      retry_if_exception_type(IncompleteReadError) |
                      retry_if_exception_type(ReadTimeoutError) |
                      retry_if_exception_type(EndpointConnectionError))

THISDIR = os.path.dirname(os.path.abspath(__file__))


def retry_callback(retry_state):
    print(f"Retrying after {retry_state.outcome.exception()}")


def runcmd(cmd: list, quiet: bool = False, **kwargs: int) -> subprocess.CompletedProcess:
    '''
    Run the given command using subprocess.run and perform verification.
    @param cmd: list that represents the command to be executed
    @param kwargs: key value pairs that represent options to run()
    '''
    try:
        # default to error on failed command
        pargs = {"check": True}
        pargs.update(kwargs)
        # capture_output is only on python 3.7+. Provide convenience here
        # until 3.7 is a baseline:
        if pargs.pop('capture_output', False):
            pargs['stdout'] = subprocess.PIPE
            pargs['stderr'] = subprocess.PIPE
        if not quiet:
            logging.info(f"Running command: {cmd}")
        cp = subprocess.run(cmd, **pargs)
    except subprocess.CalledProcessError as e:
        logging.error("Command returned bad exitcode")
        logging.error(f"COMMAND: {cmd}")
        if e.stdout:
            logging.error(f" STDOUT: {e.stdout.decode()}")
        if e.stderr:
            logging.error(f" STDERR: {e.stderr.decode()}")
        raise e
    return cp  # subprocess.CompletedProcess


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
            elif isinstance(x[k], dict) and isinstance(y[k], dict):
                # recursively merge
                ret.update({k: merge_dicts(x[k], y[k])})
            elif isinstance(x[k], list) and isinstance(y[k], list):
                ret.update({k: x[k]})
                merge_lists(ret, y, k)
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

    with Lock(lock_path, lifetime=LOCK_DEFAULT_LIFETIME):
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
        lock = Lock(lock_path, lifetime=LOCK_DEFAULT_LIFETIME)
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
        t = datetime.datetime.now(datetime.UTC)
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


def extract_image_json(workdir, commit):
    with Lock(os.path.join(workdir, 'tmp/image.json.lock'),
              lifetime=LOCK_DEFAULT_LIFETIME):
        repo = os.path.join(workdir, 'tmp/repo')
        path = os.path.join(workdir, 'tmp/image.json')
        tmppath = path + '.tmp'
        with open(tmppath, 'w') as f:
            rc = subprocess.call(['ostree', f'--repo={repo}', 'cat', commit, '/usr/share/coreos-assembler/image.json'], stdout=f)
            if rc == 0:
                # Happy path, we have image.json in the ostree commit, rename it into place and we're done.
                os.rename(tmppath, path)
                return
        # Otherwise, we are operating on a legacy build; clean up our tempfile.
        os.remove(tmppath)
        if not os.path.isfile(path):
            # In the current build system flow, image builds will have already
            # regenerated tmp/image.json from src/config.  If that doesn't already
            # exist, then something went wrong.
            raise Exception("Failed to extract image.json")
        else:
            # Warn about this case; but it's not fatal.
            print("Warning: Legacy operating on ostree image that does not contain image.json")


# In coreos-assembler, we are strongly oriented towards the concept of a single
# versioned "build" object that has artifacts.  But rpm-ostree (among other things)
# really natively wants to operate on unpacked ostree repositories.  So, we maintain
# a `tmp/repo` (along with `cache/repo-build`) that are treated as caches.
# In some cases, such as building a qemu image, then later trying to generate
# a metal image, we may not have preserved that cache.
#
# Call this function to ensure that the ostree commit for a given build is in tmp/repo.
#
# Note also a user can request a partial import where just the commit object is
# imported. This is a really lightweight way to get basic information like
# version/commit metadata and enables things like rpm-ostree db diff.
def import_ostree_commit(workdir, buildpath, buildmeta, extract_json=True, partial_import=False):
    if extract_json and partial_import:
        raise Exception("can't extract json from a partial import")
    tmpdir = os.path.join(workdir, 'tmp')
    with Lock(os.path.join(workdir, 'tmp/repo.import.lock'),
              lifetime=LOCK_DEFAULT_LIFETIME):
        repo = os.path.join(tmpdir, 'repo')
        commit = buildmeta['ostree-commit']
        was_oci_imported = buildmeta.get('coreos-assembler.oci-imported', False)
        tarfile = os.path.join(buildpath, buildmeta['images']['ostree']['path'])
        # create repo in case e.g. tmp/ was cleared out; idempotent
        subprocess.check_call(['ostree', 'init', '--repo', repo, '--mode=archive'])

        # in the common case where we're operating on a recent build (or
        # recently imported OCI image), the OSTree commit should already be in
        # the tmprepo
        commitpartial = os.path.join(repo, f'state/{commit}.commitpartial')
        if (subprocess.call(['ostree', 'show', '--repo', repo, commit],
                            stdout=subprocess.DEVNULL,
                            stderr=subprocess.DEVNULL) == 0):
            if os.path.isfile(commitpartial):
                if partial_import:
                    # We have a partial commit (just the object), but the user only
                    # requested a partial import so that's OK. We can return.
                    return
            else:
                # We have the full commit. We can extract the json if requested and return.
                if extract_json:
                    extract_image_json(workdir, commit)
                return

        # If the user only requested a partial import then we'll just "import" the
        # commit object itself into the repo.
        if partial_import:
            print(f"Importing {commit} object (partial import)")
            commitobject = os.path.join(buildpath, 'ostree-commit-object')
            commitpath = os.path.join(repo, f'objects/{commit[:2]}/{commit[2:]}.commit')
            os.makedirs(os.path.dirname(commitpath), exist_ok=True)
            shutil.copy(commitobject, commitpath)
            open(commitpartial, 'w').close()
            return

        print(f"Extracting {commit}")
        assert tarfile.endswith('.ociarchive')
        # We do this in two stages, because right now ex-container only writes to
        # non-archive repos.  Also, in the privileged case we need sudo to write
        # to `repo-build`, though it might be good to change this by default.
        if was_oci_imported:
            # This was initially imported using `cosa import`. Go through that
            # path again because it's not an encapsulated commit.
            import_oci_archive(tmpdir, tarfile, buildmeta['buildid'])
        elif os.environ.get('COSA_PRIVILEGED', '') == '1':
            build_repo = os.path.join(repo, '../../cache/repo-build')
            # note: this actually is the same as `container unencapsulate` and
            # so only works with "pure OSTree OCI" encapsulated commits (legacy path)
            subprocess.check_call(['sudo', 'ostree', 'container', 'import', '--repo', build_repo,
                                   '--write-ref', buildmeta['buildid'],
                                   'ostree-unverified-image:oci-archive:' + tarfile])
            subprocess.check_call(['sudo', 'ostree', f'--repo={repo}', 'pull-local', build_repo, buildmeta['buildid']])
            uid = os.getuid()
            gid = os.getgid()
            subprocess.check_call(['sudo', 'chown', '-hR', f"{uid}:{gid}", repo])
        else:
            with tempfile.TemporaryDirectory(dir=tmpdir) as tmpd:
                subprocess.check_call(['ostree', 'init', '--repo', tmpd, '--mode=bare-user'])
                subprocess.check_call(['ostree', 'container', 'import', '--repo', tmpd,
                                       '--write-ref', buildmeta['buildid'],
                                       'ostree-unverified-image:oci-archive:' + tarfile])
                subprocess.check_call(['ostree', f'--repo={repo}', 'pull-local', tmpd, buildmeta['buildid']])

        # Also extract image.json since it's commonly needed by image builds
        if extract_json:
            extract_image_json(workdir, commit)


def import_oci_archive(parent_tmpd, ociarchive, ref):
    '''
    Imports layered/non-encapsulated OCI archive into the tmp/repo. Returns
    the OSTree commit that was imported.
    '''
    with tempfile.TemporaryDirectory(dir=parent_tmpd) as tmpd:
        subprocess.check_call(['ostree', 'init', '--repo', tmpd, '--mode=bare-user'])

        # Init tmp/repo in case it doesn't exist.
        # If it exists, no problem. It's idempotent
        subprocess.check_call(['ostree', 'init', '--repo', 'tmp/repo', '--mode=archive'])

        # import all the blob refs for more efficient import into bare-user repo
        blob_refs = subprocess.check_output(['ostree', 'refs', '--repo', 'tmp/repo',
                                             '--list', 'ostree/container/blob'],
                                            encoding='utf-8').splitlines()
        if len(blob_refs) > 0:
            subprocess.check_call(['ostree', 'pull-local', '--repo', tmpd, 'tmp/repo'] + blob_refs)

        subprocess.check_call(['ostree', 'container', 'image', 'pull', tmpd,
                               f'ostree-unverified-image:oci-archive:{ociarchive}'])

        # awkwardly work around the fact that there is no --write-ref equivalent
        # XXX: we can make this better once we can rely on --ostree-digestfile
        # https://github.com/bootc-dev/bootc/pull/1421
        refs = subprocess.check_output(['ostree', 'refs', '--repo', tmpd,
                                        '--list', 'ostree/container/image'],
                                       encoding='utf-8').splitlines()
        assert len(refs) == 1
        subprocess.check_call(['ostree', 'refs', '--repo', tmpd, refs[0], '--create', ref])
        subprocess.check_call(['ostree', 'refs', '--repo', 'tmp/repo', ref, '--delete'])
        subprocess.check_call(['ostree', 'pull-local', '--repo', 'tmp/repo', tmpd, ref])

        # export back all the blob refs for more efficient imports of next builds
        blob_refs = subprocess.check_output(['ostree', 'refs', '--repo', tmpd,
                                             '--list', 'ostree/container/blob'],
                                            encoding='utf-8').splitlines()
        subprocess.check_call(['ostree', 'pull-local', '--repo', 'tmp/repo', tmpd] + blob_refs)

    ostree_commit = subprocess.check_output(['ostree', 'rev-parse', '--repo', 'tmp/repo', ref], encoding='utf-8').strip()
    return ostree_commit


def get_basearch():
    try:
        return get_basearch.saved
    except AttributeError:
        get_basearch.saved = RpmOstree.get_basearch()
        return get_basearch.saved


def parse_fcos_version_to_timestamp(version):
    '''
    Parses an FCOS build ID and verifies the versioning is accurate. Then
    it verifies that the parsed timestamp has %Y%m%d format and returns that.
    Also, parses older format for versions, for eg. 30.20190712.0
    '''
    m = re.match(r'^([0-9]{2})\.([0-9]{8})\.([0-9]+|dev)(?:\.([0-9]+))?$', version)
    if m is None:
        raise Exception(f"Incorrect versioning for FCOS build {version}")
    try:
        timestamp = datetime.datetime.strptime(m.group(2), '%Y%m%d')
    except ValueError:
        raise Exception(f"FCOS build {version} has incorrect date format. It should be in (%Y%m%d)")
    return timestamp


def convert_duration_to_days(duration_arg):
    """
    Parses duration strings and convert them into days.
    The excpected format is Nd/D, nw/W, Nm/M, Ny/Y where N is a positive integer.
    The return value is the number of days represented, in integer format
    """
    match = re.match(r'^([0-9]+)([dDmMyYwW])$', duration_arg)

    if match is None:
        raise ValueError(f"Incorrect duration '{duration_arg}'. Valid values are in the form of 1d, 2w, 3m, 4y")

    unit = match.group(2)
    value = int(match.group(1))
    match unit.lower():
        case "y":
            days = value * 365
        case "m":
            days = value * 30
        case "w":
            days = value * 7
        case "d":
            days = value
        case _:
            raise ValueError(f"Invalid unit '{match.group(2)}'. Please use y (years), m (months), w (weeks), or d (days).")
    return days


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
    dt = datetime.datetime.strptime(date_string, '%Y-%m-%dT%H:%M:%SZ')
    return dt.replace(tzinfo=datetime.timezone.utc)


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
        out = json.loads(runcmd(
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


def generate_image_json(srcfile, ostree_manifest):
    manifest_vars = yaml.safe_load(open(ostree_manifest))['variables']
    r = yaml.safe_load(open("/usr/lib/coreos-assembler/image-default.yaml"))
    for k, v in flatten_image_yaml(srcfile, format_args=manifest_vars).items():
        r[k] = v
    return r


def write_image_json(srcfile, outfile, ostree_manifest):
    r = generate_image_json(srcfile, ostree_manifest)
    with open(outfile, 'w') as f:
        json.dump(r, f, sort_keys=True)


# Merge two lists, avoiding duplicates. Exact duplicate kargs could be valid
# but we have no use case for them right now in our official images.
def merge_lists(x, y, k):
    x[k] = x.get(k, [])
    assert isinstance(x[k], list)
    y[k] = y.get(k, [])
    assert isinstance(y[k], list)
    x[k].extend([i for i in y[k] if i not in x[k]])


def flatten_image_yaml(srcfile, base=None, format_args={}):
    if base is None:
        base = {}

    with open(srcfile) as f:
        contents = f.read()
        srcyaml = yaml.safe_load(contents.format(**format_args))

    # first, special-case list values
    merge_lists(base, srcyaml, 'extra-kargs')

    # then handle all the non-list values
    base = merge_dicts(base, srcyaml)

    if 'include' not in srcyaml:
        return base

    fn = os.path.join(os.path.dirname(srcfile), srcyaml['include'])
    del base['include']
    return flatten_image_yaml(fn, base=base, format_args=format_args)


def ensure_glob(pathname, **kwargs):
    '''Call glob.glob(), and fail if there are no results.'''
    ret = glob.glob(pathname, **kwargs)
    if not ret:
        raise Exception(f'No matches for {pathname}')
    return ret


def ncpu():
    '''Return the number of usable CPUs we have for parallelism.'''
    return int(subprocess.check_output(['kola', 'ncpu']))
