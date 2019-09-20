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
import gi
import semver

gi.require_version("RpmOstree", "1.0")
from gi.repository import RpmOstree

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


# Obviously this is a hack but...we need to know this before
# launching, and I don't think we have structured metadata in e.g. qcow2.
# There are other alternatives but we'll carry this hack for now.
# But if you're reading this comment 10 years in the future, I won't be
# too surprised either ;)  Oh and hey if you are please send me an email, it'll
# be like a virtual time capsule!  If they still use email then...
def disk_ignition_version(path):
    bn = os.path.basename(path)
    if bn.startswith(("rhcos-41", "rhcos-42", "rhcos-43")):
        return "2.2.0"
    else:
        return "3.0.0"


def import_ostree_commit(repo, commit, tarfile):
    # create repo in case e.g. tmp/ was cleared out; idempotent
    subprocess.check_call(['ostree', 'init', '--repo', repo, '--mode=archive'])

    # in the common case where we're operating on a recent build, the OSTree
    # commit should already be in the tmprepo
    commitpartial = os.path.join(repo, f'state/{commit}.commitpartial')
    if (subprocess.call(['ostree', 'show', '--repo', repo, commit],
                        stdout=subprocess.DEVNULL,
                        stderr=subprocess.DEVNULL) == 0
            and not os.path.isfile(commitpartial)):
        return

    with tempfile.TemporaryDirectory(dir=f'{repo}/tmp') as d:
        subprocess.check_call(['tar', '-C', d, '-xf', tarfile])
        subprocess.check_call(['ostree', 'pull-local', '--repo', repo,
                               d, commit])


def get_basearch():
    try:
        return get_basearch.saved
    except AttributeError:
        get_basearch.saved = RpmOstree.get_basearch()
        return get_basearch.saved


# FIXME: Add tests
class Builds:  # pragma: nocover
    def __init__(self, workdir=None):
        self._workdir = workdir
        self._fn = self._path("builds/builds.json")
        if not os.path.isdir(self._path("builds")):
            raise Exception("No builds/ dir found!")
        elif os.path.isfile(self._fn):
            self._data = load_json(self._fn)
        else:
            # must be a new workdir; use new schema
            self._data = {
                'schema-version': "1.0.0",
                'builds': []
            }
            self.flush()
        self._version = semver.parse_version_info(
            self._data.get('schema-version', "0.0.1"))
        # we understand < 2.0.0 only
        if self._version._major >= 2:
            raise Exception("Builds schema too new; please update cosa")
        # for now, since we essentially just support "1.0.0" and "0.0.1",
        # just dillute to a bool
        self._legacy = (self._version._major < 1)

    def _path(self, path):
        if not self._workdir:
            return path
        return os.path.join(self._workdir, path)

    def has(self, build_id):
        if self._legacy:
            return build_id in self._data['builds']
        return any([b['id'] == build_id for b in self._data['builds']])

    def is_empty(self):
        return len(self._data['builds']) == 0

    def get_latest(self):
        # just let throw if there are none
        if self._legacy:
            return self._data['builds'][0]
        return self._data['builds'][0]['id']

    def get_build_arches(self, build_id):
        assert not self._legacy
        for build in self._data['builds']:
            if build['id'] == build_id:
                return build['arches']
        assert False, "Build not found!"

    def get_build_dir(self, build_id, basearch=None):
        if build_id == 'latest':
            build_id = self.get_latest()
        if self._legacy:
            return self._path(f"builds/{build_id}")
        if not basearch:
            # just assume caller wants build dir for current arch
            basearch = get_basearch()
        return self._path(f"builds/{build_id}/{basearch}")

    def insert_build(self, build_id, basearch=None):
        if self._legacy:
            self._data['builds'].insert(0, build_id)
        else:
            if not basearch:
                basearch = get_basearch()
            # for future tooling: allow inserting in an existing build for a
            # separate arch
            for build in self._data['builds']:
                if build['id'] == build_id:
                    if basearch in build['arches']:
                        raise "Build {build_id} for {basearch} already exists"
                    build['arches'] += [basearch]
                    break
            else:
                self._data['builds'].insert(0, {
                    'id': build_id,
                    'arches': [
                        basearch
                    ]
                })

    def bump_timestamp(self):
        self._data['timestamp'] = rfc3339_time()
        self.flush()

    def is_legacy(self):
        return self._legacy

    def raw(self):
        return self._data

    def flush(self):
        write_json(self._fn, self._data)
