import json
import os.path

from cosalib.builds import Builds
from cosalib.cmdlib import (
    load_json,
    write_json)


class GenericBuildMeta(dict):
    """
    GenericBuildMeta interacts with a builds meta.json
    """

    def __init__(self, workdir=None, build='latest'):
        builds = Builds(workdir)
        if build != "latest":
            if not builds.has(build):
                raise Exception('Build was not found in builds.json')
        else:
            build = builds.get_latest()

        self._meta_path = os.path.join(
            builds.get_build_dir(build), 'meta.json')
        self.read()

    @property
    def path(self):
        return self._meta_path

    def read(self):
        """
        Read the meta.json file into this object instance.
        """
        # Remove any current data
        self.clear()
        # Load the file
        self.update(load_json(self._meta_path))

    def write(self):
        """
        Write out the dict to the meta path.
        """
        write_json(self._meta_path, dict(self))

    def get(self, *args):
        """
        Extend dict.get() to support nested valued. The first argument is a
        list, then it will be treated as a nested get. For example:
            Given: {'a': {'b': 'c'}}
               self.get(['a','b'], None) will return 'c'
               self.get('a') will return {'b': 'c'}
               self.get(['a','b','c','d'], 'nope') will return 'nope'

        :param args: Ordered key path
        :type args: list
        :returns: The value of the key
        :rtype: any
        """
        path = args[0]
        default = None if len(args) == 1 else args[1]

        if not isinstance(path, list):
            return super().get(*args)

        try:
            haystack = dict(self)
            for arg in path:
                haystack = haystack[arg]
            return haystack
        except KeyError:
            return default

    def set(self, pathing, value):
        """
        Sets key path to a value.

        :param pathing: Ordered key path
        :type pathing: list
        :param value: The value to use
        :type value: any
        :raises: IndexError, Exception
        """
        if not isinstance(pathing, list):
            self[pathing] = value
            return

        updated = False
        if len(pathing) == 1:
            self[pathing[0]] = value
            return
        loc = dict(self)
        for p in pathing:
            if isinstance(loc[p], dict):
                loc = loc[p]
            else:
                loc[p] = value
                updated = True
                break
        if updated is False:
            raise Exception('Unable to set {key} to {value}')

    def __str__(self):
        """
        Returns the entire structure in a pretty json string format.

        :returns: The meta structure
        :rtype: dict
        """
        return json.dumps(dict(self), indent=4)
