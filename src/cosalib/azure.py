import os
from cosalib.cmdlib import run_verbose
from tenacity import (
    retry,
    stop_after_attempt
)


@retry(stop=stop_after_attempt(3))
def remove_azure_image(image, resource_group, auth, profile):
    print(f"Azure: removing image {image}")
    try:
        run_verbose([
            'ore', 'azure',
            '--azure-auth', auth,
            '--azure-profile', profile,
            'delete-image-arm',
            '--image-name', image,
            '--resource-group', resource_group
        ])
    except SystemExit:
        raise Exception("Failed to remove image")


@retry(stop=stop_after_attempt(3))
def azure_run_ore(*args):
    print("""
Azure currently does not produce virtual machine
registrations. This command is a place-holder only.
""")


@retry(stop=stop_after_attempt(3))
def azure_run_ore_replicate(*args):
    print("""
Azure currently does not produce virtual machine
registrations. This command is a place-holder only.
""")


def azure_cli(parser):
    """
    Common Azure CLI
    """
    parser.add_argument(
        '--auth',
        help='Path to Azure auth file',
        default=os.environ.get("AZURE_AUTH"))
    parser.add_argument(
        '--container',
        help='Storage location to write to',
        default=os.environ.get("AZURE_CONTAINER")
    )
    parser.add_argument(
        '--location',
        help='Azure location (default westus)',
        default=os.environ.get("AZURE_LOCATION", "westus")
    )
    parser.add_argument(
        '--profile',
        help='Path to Azure profile',
        default=os.environ.get('AZURE_PROFILE')
    )
    parser.add_argument(
        '--resource-group',
        help='Resource group',
        default=os.environ.get('AZURE_RESOURCE_GROUP')
    )
    parser.add_argument(
        '--storage-account',
        help='Storage account',
        default=os.environ.get('AZURE_STORAGE_ACCOUNT')
    )

    return parser
