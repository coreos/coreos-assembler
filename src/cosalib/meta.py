from cosalib.build import _Build

class GenericBuildMeta(_Build):
    """
        GenericBuildMeta  interacts with a builds meta.json
    """

    def __init__(self, build_dir, build, tmpbuilddir):
        self.tmpbuilddir = tmpbuilddir
        _Build.__init__(self, build_dir, build)

    def _build_artifacts(self, *args, **kwargs):
        raise NotImplementedError("MetaBuild does not understand artifacts")
