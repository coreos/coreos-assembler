import boto3
import json
import os
import subprocess
import sys

from cosalib.cmdlib import (
    flatten_image_yaml,
    retry_boto_exception,
    retry_callback,
    retry_stop
)
from tenacity import (
    retry,
    stop_after_attempt
)


@retry(stop=retry_stop, retry=retry_boto_exception,
       before_sleep=retry_callback)
def deregister_ami(ami_id, region):
    print(f"AWS: deregistering AMI {ami_id} in {region}")
    ec2 = boto3.client('ec2', region_name=region)
    ec2.deregister_image(ImageId=ami_id)


@retry(stop=retry_stop, retry=retry_boto_exception,
       before_sleep=retry_callback)
def delete_snapshot(snap_id, region):
    print(f"AWS: removing snapshot {snap_id} in {region}")
    ec2 = boto3.client('ec2', region_name=region)
    ec2.delete_snapshot(SnapshotId=snap_id)


@retry(reraise=True, stop=stop_after_attempt(3))
def aws_run_ore_replicate(build, args):
    build.refresh_meta()
    buildmeta = build.meta
    if len(buildmeta.get('amis', [])) < 1:
        raise SystemExit(("buildmeta doesn't contain source AMIs."
                         " Run buildextend-aws --upload first"))

    # Determine which region to copy from
    if not args.source_region:
        args.source_region = buildmeta['amis'][0]['name']

    # If no region specified then autodetect the regions to replicate to.
    # Specify --region=args.source_region here so ore knows to talk to
    # a region that exists (i.e. it will talk to govcloud if copying
    # from a govcloud region).
    if not args.region:
        args.region = subprocess.check_output([
            'ore', 'aws', '--region', args.source_region, 'list-regions'
        ]).decode().strip().split()

    # only replicate to regions that don't already exist
    existing_regions = [item['name'] for item in buildmeta['amis']]
    duplicates = list(set(args.region).intersection(existing_regions))
    if len(duplicates) > 0:
        print((f"AMIs already exist in {duplicates} region(s), "
               "skipping listed region(s)..."))

    region_list = list(set(args.region) - set(duplicates))
    if len(region_list) == 0:
        print("no new regions detected")
        sys.exit(0)

    source_image = None
    for a in buildmeta['amis']:
        if a['name'] == args.source_region:
            source_image = a['hvm']
            break

    if source_image is None:
        raise Exception(("Unable to find AMI ID for "
                        f"{args.source_region} region"))

    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])
    ore_args.extend([
        'aws', 'copy-image', '--image',
        source_image, '--region', args.source_region
    ])
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
                buildmeta['amis'].extend(ami_data)

            # Record the AMI's that have been replicated as they happen.
            # When re-running the replication, we don't want to be lose
            # what has been done.
            build.meta_write()


@retry(reraise=True, stop=stop_after_attempt(3))
def aws_run_ore(build, args):
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
    # Capture any input from image.yaml
    image_yaml = flatten_image_yaml(
        '/usr/lib/coreos-assembler/image-default.yaml',
        flatten_image_yaml('src/config/image.yaml')
    )
    if 'aws-imdsv2-only' in image_yaml and image_yaml['aws-imdsv2-only']:
        ore_args.extend(['--imdsv2-only'])
    if 'aws-volume-type' in image_yaml:
        ore_args.extend(['--volume-type', image_yaml['aws-volume-type']])
    if 'aws-x86-boot-mode' in image_yaml:
        ore_args.extend(['--x86-boot-mode', image_yaml['aws-x86-boot-mode']])

    ore_args.extend([
        '--region', f"{region}",
        '--bucket', f"{args.bucket}",
        '--ami-name', f"{build.build_name}-{build.build_id}-{build.basearch}",
        '--name', f"{build.build_name}-{build.build_id}-{build.basearch}",
        '--ami-description', f"{build.summary} {build.build_id} {build.basearch}",
        '--file', f"{build.image_path}",
        '--arch', f"{build.basearch}",
        '--disk-size-inspect',
        '--delete-object'
    ])
    for user in args.grant_user:
        ore_args.extend(['--grant-user', user])
    for user in args.grant_user_snapshot:
        ore_args.extend(['--grant-user-snapshot', user])
    if args.public:
        ore_args.extend(['--public'])

    print("+ {}".format(subprocess.list2cmdline(ore_args)))
    ore_data = json.loads(subprocess.check_output(ore_args))

    # This matches the Container Linux schema:
    # https://stable.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json
    ami_data = build.meta.get("amis", [])
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

    build.meta['amis'] = ami_data
    build.meta_write()


def aws_cli(parser):
    parser.add_argument("--bucket", help="S3 Bucket")
    parser.add_argument("--credentials-file", help="AWS config file",
                        default=os.environ.get("AWS_CONFIG_FILE"))
    parser.add_argument("--name-suffix", help="Suffix for name")
    parser.add_argument("--grant-user", help="Grant user launch permission",
                        nargs="*", default=[])
    parser.add_argument("--grant-user-snapshot", help="Grant user snapshot volume permission",
                        nargs="*", default=[])
    parser.add_argument("--public", action="store_true", help="Mark images as publicly available")
    return parser
