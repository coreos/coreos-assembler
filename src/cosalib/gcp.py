import os
import re
import urllib
from cosalib.cmdlib import run_verbose
from tenacity import (
    retry,
    stop_after_attempt
)


# This is the naming rule used by GCP and is used to check image
# names during upload. See:
# https://cloud.google.com/compute/docs/reference/rest/v1/images/insert
GCP_NAMING_RE = r"[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?|[1-9][0-9]{0,19}"


@retry(reraise=True, stop=stop_after_attempt(3))
def remove_gcp_image(gcp_id, json_key, project):
    print(f"GCP: removing image {gcp_id}")
    try:
        run_verbose([
            'ore', 'gcloud', 'delete-images', gcp_id,
            '--json-key', json_key,
            '--project', project
        ])
    except SystemExit:
        raise Exception("Failed to remove image")


@retry(reraise=True, stop=stop_after_attempt(3))
def gcp_run_ore(build, args):
    """
    Execute ore to upload the tarball and register the image
    """
    arg_exp_str = "parameter '--{}' or envVar '{}' must be defined"
    if args.bucket is None:
        raise Exception(arg_exp_str.format("bucket", "GCP_BUCKET"))
    if args.json_key is None:
        raise Exception(arg_exp_str.format("json-key", "GCP_JSON_AUTH"))
    if args.project is None:
        raise Exception(arg_exp_str.format("project", "GCP_PROJECT"))

    ore_args = ['ore']
    if args.log_level == "DEBUG":
        ore_args.extend(['--log-level', "DEBUG"])

    gcp_name = re.sub(r'[_\.]', '-', build.image_name_base)
    if not re.fullmatch(GCP_NAMING_RE, gcp_name):
        raise Exception(f"{gcp_name} does match the naming rule: file a bug")

    ore_args.extend([
        'gcloud',
        '--project', args.project,
        '--basename', build.build_name,
        'upload',
        '--force',  # We want to support restarting the pipeline
        '--board=""',
        '--bucket', f'gs://{args.bucket}/{build.build_name}',
        '--json-key', args.json_key,
        '--name', gcp_name,
        '--file', f"{build.image_path}",
    ])

    run_verbose(ore_args)
    url_path = urllib.parse.quote((
        "storage.googleapis.com/"
        f"{args.bucket}/{build.build_name}/{build.image_name}"
    ))
    build.meta['gcp'] = {
        'image': gcp_name,
        'url': f"https://{url_path}",
    }


def gcp_run_ore_replicate(*args, **kwargs):
    print("""
Google Cloud Compute Engine does not require regional
replication. This command is a place-holder only.
""")


def gcp_cli(parser):
    """
    Extend a parser with the GCP options
    """
    parser.add_argument("--bucket",
                        help="Storage account to write image to",
                        default=os.environ.get("GCP_BUCKET"))
    parser.add_argument("--gce",
                        help="Use GCE as the platform ID instead of GCP",
                        action="store_true",
                        default=bool(
                            os.environ.get("GCP_GCE_PLATFORM_ID", False))
                        )
    parser.add_argument("--json-key",
                        help="GCP Service Account JSON Auth",
                        default=os.environ.get("GCP_JSON_AUTH"))
    parser.add_argument("--name-suffix",
                        help="Append suffix to name",
                        required=False)
    parser.add_argument("--project",
                        help="GCP Project name",
                        default=os.environ.get("GCP_PROJECT_NAME"))
    return parser
