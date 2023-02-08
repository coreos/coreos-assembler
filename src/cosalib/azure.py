import os
import urllib
from cosalib.cmdlib import runcmd
from tenacity import (
    retry,
    stop_after_attempt
)


@retry(reraise=True, stop=stop_after_attempt(3))
def remove_azure_image(image, resource_group, credentials):
    print(f"Azure: removing image {image}")
    try:
        runcmd([
            'ore', 'azure',
            '--azure-credentials', credentials,
            'delete-image',
            '--image-name', image,
            '--resource-group', resource_group
        ])
    except SystemExit:
        raise Exception("Failed to remove image")


@retry(reraise=True, stop=stop_after_attempt(3))
def azure_run_ore(build, args):
    """
    Execute ore to upload the vhd image in blob format
    See:
      - https://github.com/coreos/mantle/#azure
      - https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction
    :param args: The command line arguments
    :type args: argparse.Namespace
    :param build: Build instance to use
    :type build: Build
    """
    azure_vhd_name = f"{build.image_name_base}.vhd"
    ore_args = [
        'ore',
        '--log-level', args.log_level,
        'azure', 'upload-blob',
        '--azure-credentials', args.credentials,
        '--azure-location', args.location,
        '--blob-name', azure_vhd_name,
        '--file', f"{build.image_path}",
        '--container', args.container,
        '--resource-group', args.resource_group,
        '--storage-account', args.storage_account
    ]
    if args.force:
        ore_args.append('--overwrite')
    runcmd(ore_args)

    url_path = urllib.parse.quote((
        f"{args.storage_account}.blob.core.windows.net/"
        f"{args.container}/{azure_vhd_name}"
    ))
    build.meta['azure'] = {
        'image': azure_vhd_name,
        'url': f"https://{url_path}",
    }
    build.meta_write()  # update build metadata


@retry(reraise=True, stop=stop_after_attempt(3))
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
        '--credentials',
        help='Path to Azure credentials file',
        default=os.environ.get("AZURE_CREDENTIALS"))
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
