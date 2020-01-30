from tenacity import (
    retry,
    stop_after_attempt
)


@retry(reraise=True, stop=stop_after_attempt(3))
def vultr_run_ore(build, args):
    """
    Placeholder to upload a raw image to Vultr.
    """
    raise Exception("not implemented yet")


def vultr_run_ore_replicate(*args, **kwargs):
    raise Exception("not implemented yet")


def vultr_cli(parser):
    """
    Extend a parser with the Vultr options
    """
    return parser
