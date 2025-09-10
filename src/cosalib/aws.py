import json
import os
import subprocess

from cosalib.builds import Builds
from cosalib.cmdlib import runcmd
from tenacity import (
    retry,
    stop_after_attempt
)


@retry(reraise=True, stop=stop_after_attempt(3))
def deregister_aws_resource(ami, snapshot, region, credentials_file):
    print(f"AWS: deregistering AMI {ami} and {snapshot} in {region}")
    try:
        runcmd([
            'ore', 'aws', 'delete-image',
            '--credentials-file', credentials_file,
            '--ami', ami,
            '--snapshot', snapshot,
            "--region", region,
            "--allow-missing"
        ])
        print(f"AWS: successfully removed {ami} and {snapshot}")
    except SystemExit:
        raise Exception(f"Failed to remove {ami} or {snapshot}")


@retry(reraise=True, stop=stop_after_attempt(3))
def aws_run_ore_replicate(build, args):
    build.refresh_meta()
    buildmeta = build.meta
    buildmeta_keys = ["amis"]
    if len(buildmeta.get(buildmeta_keys[0], [])) < 1:
        raise SystemExit(("buildmeta doesn't contain source AMIs."
                         " Run buildextend-aws --upload first"))

    if len(buildmeta.get('aws-winli', [])) > 0:
        buildmeta_keys.append("aws-winli")

    for key in buildmeta_keys:
        # Determine which region to copy from
        if not args.source_region:
            args.source_region = buildmeta[key][0]['name']

        ore_args = ['ore', 'aws', '--region', args.source_region]
        if args.log_level:
            ore_args.extend(['--log-level', args.log_level])
        if args.credentials_file:
            ore_args.extend(['--credentials-file', args.credentials_file])

        # If no region specified then autodetect the regions to replicate to.
        # Specify --region=args.source_region here so ore knows to talk to
        # a region that exists (i.e. it will talk to govcloud if copying
        # from a govcloud region).
        if not args.region:
            args.region = subprocess.check_output(
                ore_args + ['list-regions']).decode().strip().split()

        # only replicate to regions that don't already exist
        existing_regions = [item['name'] for item in buildmeta[key]]
        duplicates = list(set(args.region).intersection(existing_regions))
        if len(duplicates) > 0:
            print((f"AMIs already exist in {duplicates} region(s), "
                   "skipping listed region(s)..."))

        region_list = list(set(args.region) - set(duplicates))
        if len(region_list) == 0:
            print("no new regions detected")
            continue
        source_image = None
        for a in buildmeta[key]:
            if a['name'] == args.source_region:
                source_image = a['hvm']
                break

        if source_image is None:
            raise Exception(("Unable to find AMI ID for "
                            f"{args.source_region} region"))

        ore_args.extend(['copy-image', '--image', source_image])
        ore_args.extend(region_list)
        print("+ {}".format(subprocess.list2cmdline(ore_args)))

        ore_data = ""
        try:
            ore_data = subprocess.check_output(ore_args, encoding='utf-8')
        except subprocess.CalledProcessError as e:
            ore_data = e.output or ""
            raise e
        finally:
            ore_data = ore_data.strip()
            if len(ore_data) > 0:
                for line in ore_data.split('\n'):
                    j = json.loads(line)
                    # This matches the Container Linux schema:
                    # https://stable.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json
                    ami_data = [{'name': region,
                                 'hvm': vals['ami'],
                                 'snapshot': vals['snapshot']}
                                for region, vals in j.items()]
                    buildmeta[key].extend(ami_data)

                # Record the AMI's that have been replicated as they happen.
                # When re-running the replication, we don't want to be lose
                # what has been done.
                build.meta_write()


@retry(reraise=True, stop=stop_after_attempt(3))
def aws_run_ore(build, args):
    # Skip the artifact check for AWS Windows License Included (WinLI) builds.
    # The build artifact is not required for WinLI.
    if (not build.have_artifact) and (not args.winli):
        raise Exception(f"Missing build artifact {build.image_path}")

    # First add the ore command to run before any options
    ore_args = ['ore', 'aws', 'upload']

    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])

    if args.force:
        ore_args.extend(['--force'])

    if args.credentials_file:
        ore_args.extend(['--credentials-file', args.credentials_file])

    region = "us-east-1"
    if args.region is not None and len(args.region) > 0:
        region = args.region[0]

    # Capture any settings from image json.
    image_json = Builds(workdir=os.getcwd()).get_build_image_json(build.build_id)
    if 'aws-imdsv2-only' in image_json and image_json['aws-imdsv2-only']:
        ore_args.extend(['--imdsv2-only'])
    if 'aws-volume-type' in image_json:
        ore_args.extend(['--volume-type', image_json['aws-volume-type']])
    if 'aws-x86-boot-mode' in image_json:
        ore_args.extend(['--x86-boot-mode', image_json['aws-x86-boot-mode']])

    assert bool(args.winli) == bool(args.winli_billing_product), \
        "--winli-billing-product and --winli must be specified together"

    if args.winli:
        winli_name = "-winli"
        winli_description = " Windows License Included"
        buildmeta_key = "aws-winli"
        buildmeta = build.meta
        source_snapshot = None
        for a in buildmeta['amis']:
            if a['name'] == region:
                source_snapshot = a['snapshot']
                break

        if source_snapshot is None:
            raise Exception(("Unable to find AMI source snapshot for "
                            f"{region} region"))
        ore_args.extend([
            '--source-snapshot', f"{source_snapshot}",
            '--billing-product-code', f"{args.winli_billing_product}"
        ])
    else:
        ore_args.extend([
            '--disk-size-inspect',
            '--file', f"{build.image_path}",
        ])
        winli_name = ""
        winli_description = ""
        buildmeta_key = "amis"

    if args.bucket:
        ore_args.extend(['--bucket', f"{args.bucket}"])

    ore_args.extend([
        '--region', f"{region}",
        '--ami-name', f"{build.build_name}{winli_name}-{build.build_id}-{build.basearch}",
        '--name', f"{build.build_name}{winli_name}-{build.build_id}-{build.basearch}",
        '--ami-description', f"{build.summary} {build.build_id} {build.basearch}{winli_description}",
        '--arch', f"{build.basearch}",
        '--delete-object'
    ])
    for user in args.grant_user:
        ore_args.extend(['--grant-user', user])
    for user in args.grant_user_snapshot:
        ore_args.extend(['--grant-user-snapshot', user])
    for tag in args.tags:
        ore_args.extend(['--tags', tag])
    if args.public:
        ore_args.extend(['--public'])

    print("+ {}".format(subprocess.list2cmdline(ore_args)))
    ore_data = json.loads(subprocess.check_output(ore_args))

    # This matches the Container Linux schema:
    # https://stable.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json
    ami_data = build.meta.get(buildmeta_key, [])
    # filter out (remove) existing entries (can happen if --force is used) from the
    # ami list that match this region.
    ami_data = [ami for ami in ami_data if ami.get('name') != region]
    ami_data.append({
        'name': region,
        'hvm': ore_data.get('HVM'),
        'snapshot': ore_data.get('SnapshotID')
    })

    if ore_data.get("HVM") is None:
        raise Exception(f"Upload to {args.region} failed: no AMI returned")
    if ore_data.get("SnapshotID") is None:
        raise Exception(f"Upload to {args.region} failed: no SnapshotID")

    build.meta[buildmeta_key] = ami_data
    build.meta_write()


def aws_cli(parser):
    parser.add_argument("--bucket", help="S3 Bucket")
    parser.add_argument("--credentials-file", help="AWS config file",
                        default=os.environ.get("AWS_CONFIG_FILE"))
    parser.add_argument("--name-suffix", help="Suffix for name")
    parser.add_argument("--grant-user", help="Grant user launch permission",
                        action='append', default=[])
    parser.add_argument("--grant-user-snapshot", help="Grant user snapshot volume permission",
                        action='append', default=[])
    parser.add_argument("--public", action="store_true", help="Mark images as publicly available")
    parser.add_argument("--tags", help="list of key=value tags to attach to the AMI",
                        action='append', default=[])
    parser.add_argument("--winli", action="store_true", help="create an AWS Windows LI Ami")
    parser.add_argument("--winli-billing-product", help="Windows billing product code used to create a Windows LI AMI")
    return parser
