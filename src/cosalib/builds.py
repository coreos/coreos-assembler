"""
Builds interacts with builds.json
"""

import json
import os
import semver
import gi

gi.require_version('OSTree', '1.0')
from gi.repository import Gio, OSTree

from cosalib.cmdlib import (
    get_basearch,
    rfc3339_time,
    load_json,
    write_json)


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
        if self._version._major < 1:
            err = f"Unsupported build metadata version {self._version}"
            raise SystemExit(err)

    def _path(self, path):
        if not self._workdir:
            return path
        return os.path.join(self._workdir, path)

    def has(self, build_id):
        return any([b['id'] == build_id for b in self._data['builds']])

    def is_empty(self):
        return len(self._data['builds']) == 0

    def get_latest(self):
        # just let throw if there are none
        return self._data['builds'][0]['id']

    def get_build_arches(self, build_id):
        for build in self._data['builds']:
            if build['id'] == build_id:
                return build['arches']
        assert False, "Build not found!"

    def get_build_dir(self, build_id, basearch=None):
        if build_id == 'latest':
            build_id = self.get_latest()
        if not basearch:
            # just assume caller wants build dir for current arch
            basearch = get_basearch()
        return self._path(f"builds/{build_id}/{basearch}")

    def insert_build(self, build_id, basearch=None):
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

    def init_build_meta_json(self, ostree_commit, destdir):
        """
        Given a new ostree version, initialize a new coreos-assembler
        build by writing a `meta.json` in destdir.
        """
        repopath = os.path.join(self._workdir, 'tmp/repo')
        r = OSTree.Repo.new(Gio.File.new_for_path(repopath))
        r.open(None)
        [_, rev] = r.resolve_rev(ostree_commit, True)
        [_, commit, _] = r.load_commit(rev)
        commitmeta = commit.get_child_value(0)
        version = commitmeta.unpack()['version']
        image_genver = 0
        buildid = version
        genver_key = 'coreos-assembler.image-genver'
        if not self.is_empty():
            previous_buildid = self.get_latest()
            metapath = self.get_build_dir(previous_buildid) + '/meta.json'
            with open(metapath) as f:
                previous_buildmeta = json.load(f)
            previous_commit = previous_buildmeta['ostree-commit']
            previous_image_genver = int(previous_buildmeta[genver_key])
            if previous_commit == ostree_commit:
                image_genver = previous_image_genver + 1
                buildid = f"{version}-{image_genver}"
        meta = {
            'buildid': buildid,
            genver_key: image_genver
        }
        with open(destdir + '/meta.json', 'w') as f:
            json.dump(meta, f)

    def bump_timestamp(self):
        self._data['timestamp'] = rfc3339_time()
        self.flush()

    def raw(self):
        return self._data

    def flush(self):
        write_json(self._fn, self._data)
