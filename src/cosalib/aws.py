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


@retry(stop=stop_after_attempt(3))
def aws_run_ore_replicate(build, args):
    build.refresh_meta()
    buildmeta = build.meta
    if len(buildmeta['amis']) < 1:
        raise SystemExit(("buildmeta doesn't contain source AMIs."
                         " Run buildextend-aws first"))
    if not args.regions:
        args.regions = subprocess.check_output(
            ['ore', 'aws', 'list-regions']
        ).decode().strip().split()

    # only replicate to regions that don't already exist
    existing_regions = [item['name'] for item in buildmeta['amis']]
    duplicates = list(set(args.regions).intersection(existing_regions))
    if len(duplicates) > 0:
        print((f"AMIs already exist in {duplicates} region(s), "
               "skipping listed region(s)..."))

    region_list = list(set(args.regions) - set(duplicates))
    if len(region_list) == 0:
        raise Exception("no new regions detected")

    source_image = buildmeta['amis'][0]['hvm']
    source_region = buildmeta['amis'][0]['name']
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])
    ore_args.extend(
        ['aws', 'copy-image', '--image',
         source_image, '--region', source_region]
    )

    upload_failed_in_region = None
    for upload_region in region_list:
        region_ore_args = ore_args.copy() + [upload_region]
        print("+ {}".format(subprocess.list2cmdline(region_ore_args)))
        try:
            ore_data = json.loads(subprocess.check_output(region_ore_args))
        except subprocess.CalledProcessError:
            upload_failed_in_region = upload_region
            break
        # This matches the Container Linux schema:
        # https://stable.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json
        ami_data = [{'name': region,
                     'hvm': vals['ami'],
                     'snapshot': vals['snapshot']}
                    for region, vals in ore_data.items()]
        buildmeta['amis'].extend(ami_data)

    build.meta["aws"] = ami_data
    build.meta_write()

    if upload_failed_in_region is not None:
        raise Exception(f"Upload failed in {upload_failed_in_region} region")


@retry(stop=stop_after_attempt(3))
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
    ami_data = build.meta.get("aws", [])
    ami_data.append({
        'name': args.region,
        'hvm': ore_data.get('HVM'),
        'snapshot': ore_data.get('SnapshotID')
    })

    if ore_data.get("HVM") is None:
        raise Exception(f"Upload to {args.region} failed: no AMI returned")
    if ore_data.get("SnapshotID") is None:
        raise Exception(f"Upload to {args.region} failed: no SnapshotID")

    build.meta["aws"] = ami_data
    build.meta_write()


def aws_cli(parser):
    parser.add_argument("--bucket", help="S3 Bucket")
    parser.add_argument("--name-suffix", help="Suffix for name")
    parser.add_argument("--grant-user", help="Grant user launch permission",
                        nargs="*", default=[])
    return parser
