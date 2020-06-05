import shutil

from cosalib.cmdlib import (
    run_verbose
)


def mutate_digitalocean(path):
    # DigitalOcean can import custom images directly from a URL, and
    # supports .gz and .bz2 compression but not .xz.  .bz2 is a bit tighter
    # but isn't used for any other artifact.  Manually gzip the artifact
    # here.  cmd-compress will skip recompressing it later.
    temp_path = f"{path}.gz"
    with open(temp_path, "wb") as fh:
        run_verbose(['gzip', '-9c', path], stdout=fh)
    shutil.move(temp_path, path)


def digitalocean_run_ore(build, args):
    print("""
Images are not published to DigitalOcean.  This command is a placeholder.
""")


def digitalocean_run_ore_replicate(*args, **kwargs):
    print("""
DigitalOcean does not require regional replication. This command is a
placeholder.
""")


def digitalocean_cli(parser):
    """
    Extend a parser with the DigitalOcean options
    """
    return parser
