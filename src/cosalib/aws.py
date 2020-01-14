import boto3
import json
import subprocess

from cosalib.cmdlib import (
    retry_stop,
    retry_boto_exception,
    retry_callback
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
    if len(buildmeta['amis']) < 1:
        raise SystemExit(("buildmeta doesn't contain source AMIs."
                         " Run buildextend-aws first"))
    if not args.region:
        args.region = subprocess.check_output([
            'ore', 'aws', 'list-regions'
        ]).decode().strip().split()

    # only replicate to regions that don't already exist
    existing_regions = [item['name'] for item in buildmeta['amis']]
    duplicates = list(set(args.region).intersection(existing_regions))
    if len(duplicates) > 0:
        print((f"AMIs already exist in {duplicates} region(s), "
               "skipping listed region(s)..."))

    region_list = list(set(args.region) - set(duplicates))
    if len(region_list) == 0:
        raise Exception("no new regions detected")

    source_image = buildmeta['amis'][0]['hvm']
    source_region = buildmeta['amis'][0]['name']
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])
    ore_args.extend([
        'aws', 'copy-image', '--image',
        source_image, '--region', source_region
    ])
    ore_args.extend(region_list)
    print("+ {}".format(subprocess.list2cmdline(ore_args)))

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
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])

    region = "us-east-1"
    if args.region is not None and len(args.region) > 0:
        region = args.region[0]

    ore_args.extend([
        'aws', 'upload',
        '--region', f"{region}",
        '--bucket', f"{args.bucket}",
        '--ami-name', f"{build.build_name}-{build.build_id}",
        '--name', f"{build.build_name}-{build.build_id}",
        '--ami-description', f'{build.summary} {build.build_id}',
        '--file', f"{build.image_path}",
        '--disk-size-inspect',
        '--delete-object',
        '--force'
    ])
    for user in args.grant_user:
        ore_args.extend(['--grant-user', user])

    print("+ {}".format(subprocess.list2cmdline(ore_args)))
    ore_data = json.loads(subprocess.check_output(ore_args))

    # This matches the Container Linux schema:
    # https://stable.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json
    ami_data = build.meta.get("amis", [])
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
    parser.add_argument("--name-suffix", help="Suffix for name")
    parser.add_argument("--grant-user", help="Grant user launch permission",
                        nargs="*", default=[])
    return parser
