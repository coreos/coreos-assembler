import json
import jsonschema
import os.path
import time

from cosalib.builds import Builds
from cosalib.cmdlib import (
    merge_dicts,
    load_json,
    write_json
)

COSA_DELAYED_MERGE = "coreos-assembler.delayed-meta-merge"
COSA_VER_STAMP = "coreos-assembler.meta-stamp"

SCHEMA_PATH = os.environ.get("COSA_META_SCHEMA",
                             "/usr/lib/coreos-assembler/v1.json")


class COSAMergeError(Exception):
    """
    Raised when unable to merge meta.json together
    """
    def __str__(self):
        return f"unable to merge: {': '.join(self.args)}"


def merge_meta(x, y):
    """
    Merge two GenericMeta's together

    This is likely over engineered, merge_dicts is safe enough.
    But to we want to be sure.

    As a reminder: merge_dicts only merges the superset

    Rules for merging:
    - the first dict should be on-disk while second is modified one
    - if the attribute checked exists on x, it must match on y.
    - which ever version is newer is merged into the older

    Note: this will NOT deal with replacing keys. For example,
          re-running an AWS replication will not update the missing
          keys.

    """

    # Only allow merging of artifacts from the same content
    # i.e. you can't add RHCOS into FCOS.
    for i in ["ostree-commit", "ostree-content-checksum",
              "coreos-assembler.image-config-checksum"]:
        if x.get(i, False) and (x.get(i) != y.get(i)):
            raise COSAMergeError(f"meta.json conflict on '{i}' "
                                 f"{x.get(i)} != {y.get(i)}")

    # Do not allow merging from different COSA code.
    cosa_git = "coreos-assembler.container-config-git"
    x_cosa = x.get(cosa_git, {}).get("commit")
    y_cosa = y.get(cosa_git, {}).get("commit")
    if x_cosa and (x_cosa != y_cosa):
        raise COSAMergeError(f"{cosa_git} version {x_cosa}"
                             f" doest not match {y_cosa}")

    ret = {}
    x_stamp = x.get(COSA_VER_STAMP, 0)
    y_stamp = y.get(COSA_VER_STAMP, 0)
    if x_stamp == y_stamp:
        # version on disk is the same as starting version
        ret = y
    elif x_stamp > y_stamp:
        # x is newer
        ret = merge_dicts(x, y)
    else:
        # y is newer
        ret = merge_dicts(y, x)

    # Update the version stamp to the current time in ns
    # For distributed builds time is pretty much the only
    # equalizer.
    ret[COSA_VER_STAMP] = time.time_ns()
    return ret


class COSAInvalidMeta(Exception):
    """
    Raised when meta.json does not validate
    """
    def __str__(self):
        return f"meta.json is or would be invalid: {': '.join(self.args)}"


class GenericMeta(dict):
    """
    GenericMeta is meta.json outside the scope of a build.
    """

    def __init__(self, *args, **kwargs):
        # Load the schema
        self._validator = None
        self._meta_path = kwargs.get("path")

        # Load the schema
        schema = kwargs.get("schema")
        if schema and schema.lower() not in ("false", "none"):
            with open(schema, 'r') as data:
                self._validator = jsonschema.Draft7Validator(
                    json.loads(data.read())
                )

        if not self.path:
            raise Exception("path not set")
        self.read()

        # add the timestamp
        if self.get(COSA_VER_STAMP) is None:
            self.set(COSA_VER_STAMP, os.stat(self.path).st_mtime)

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
        # Read under a lock to prevent race conditions
        self.update(load_json(self.path))
        self.validate()

    def write(self, artifact_name=None, merge_func=merge_meta, final=False):
        """
        Write out the dict to the meta path.

        """
        self.validate()
        if artifact_name is None:
            artifact_name = f"{time.time_ns()}"

        path = self.path

        # support writing meta.json to meta.<ARTIFACT>.json
        if self.get(COSA_DELAYED_MERGE) and not final:
            dn = os.path.dirname(self.path)
            path = os.path.join(dn, f"meta.{artifact_name}.json")
            self.set(COSA_VER_STAMP, time.time_ns())
            merge_func = None

        write_json(path, dict(self), merge_func=merge_func)
        self.read()
        return path

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

    def get_artifact_meta(self, artifact, unmerged=False):
        """
        Return just a dict of the artifact
        """
        data = self.dict()
        dn = os.path.dirname(self.path)
        alt_path = os.path.join(dn, f"meta.{artifact}.json")
        if (os.path.exists(alt_path) and unmerged is True and
           self.get(COSA_DELAYED_MERGE) is True):
            data = load_json(alt_path)

        return {
            "images": {
                artifact: data.get("images", {}).get(artifact, {})
            },
            artifact: data.get(artifact)
        }

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


class GenericBuildMeta(GenericMeta):
    """
    GenericBuildMeta interacts with a builds meta.json
    in a Build Context

    Yes, this OOP. Deal with it.
    """

    def __init__(self, workdir=None, build='latest',
                 basearch=None, schema=SCHEMA_PATH):
        builds = Builds(workdir)
        if build != "latest":
            if not builds.has(build):
                raise Exception('Build was not found in builds.json')
        else:
            build = builds.get_latest()

        self._build_dir = \
            builds.get_build_dir(build,
                                 basearch=basearch)
        path = os.path.join(self._build_dir, 'meta.json')
        super().__init__(schema=schema, path=path)

    @property
    def build_dir(self):
        return self._build_dir
