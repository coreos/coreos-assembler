import json
import jsonschema
import os.path

from cosalib.builds import Builds
from cosalib.cmdlib import write_json


SCHEMA_PATH = os.environ.get("COSA_META_SCHEMA",
                             "/usr/lib/coreos-assembler/schema/v1.json")


class COSAInvalidMeta(Exception):
    """
    Raised when meta.json does not validate
    """
    def __str__(self):
        return f"meta.json is or would be invalid: {': '.join(self.args)}"


class GenericBuildMeta(dict):
    """
    GenericBuildMeta interacts with a builds meta.json
    """

    def __init__(self, workdir=None, build='latest',
                 schema=SCHEMA_PATH):
        builds = Builds(workdir)
        if build != "latest":
            if not builds.has(build):
                raise Exception('Build was not found in builds.json')
        else:
            build = builds.get_latest()

        # Load the schema
        self._validator = None
        if schema and schema.lower() not in ("false", "none"):
            with open(schema, 'r') as data:
                self._validator = jsonschema.Draft7Validator(
                    json.loads(data.read())
                )

        self._meta_path = os.path.join(
            builds.get_build_dir(build), 'meta.json')
        self.read()

    @property
    def path(self):
        return self._meta_path

    def validate(self):
        """
        validate ensures that the meta structure matches the schema
        expected.
        """
        if not self._validator:
            return
        self._validator.validate(dict(self))

    def read(self):
        """
        Read the meta.json file into this object instance.
        """
        # Remove any current data
        self.clear()
        # Load the file and record the initial timestamp to
        # detect conflicts
        with open(self._meta_path) as f:
            self._initial_timestamp = os.fstat(f.fileno()).st_mtime
            self.update(json.load(f))
        self.validate()

    def write(self):
        """
        Write out the dict to the meta path.
        """
        self.validate()
        ts = os.stat(self._meta_path).st_mtime
        if ts != self._initial_timestamp:
            raise Exception(f"Detected read-modify-write conflict, expected timestamp={self._initial_timestamp} found {ts}")
        write_json(self._meta_path, dict(self))
        self.read()

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

    def dict(self):
        return dict(self)

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
