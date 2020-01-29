from tenacity import (
    retry,
    stop_after_attempt
)


@retry(reraise=True, stop=stop_after_attempt(3))
def vultr_run_ore(build, args):
    """
    Placeholder to upload a raw image to Vultr.
    """
    pass


def vultr_run_ore_replicate(*args, **kwargs):
    print("Images are not published to Vultr. This is a placeholder")


def vultr_cli(parser):
    """
    Extend a parser with the Vultr options
    """
    return parser
