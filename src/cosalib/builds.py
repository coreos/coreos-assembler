"""
Builds interacts with builds.json
"""

import os
import semver

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
