import boto3
import logging as log
import json
import subprocess

from multiprocessing import (
    Manager,
    Process
)
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
def _replicate_worker(dest_region, shared_meta):
    args = shared_meta.get('args')
    ore_args = [
        'ore',
        f'--log-level={args.log_level}',
        'aws', 'copy-image',
        '--image', shared_meta['source_image'],
        '--region', shared_meta['source_region'],
        dest_region,
    ]

    try:
        print("+ {}".format(ore_args))
        ore_data = json.loads(subprocess.check_output(ore_args))
    except subprocess.CalledProcessError as e:
        log.critical(f"failed replication in {dest_region}: {e}")
        shared_meta[dest_region] = e
        # this should trigger a retry
        raise e

    shared_meta[dest_region] = [
        {'name': name,
         'hvm': vals['ami'],
         'snapshot': vals['snapshot']}
        for name, vals in ore_data.items()]


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

    # Create a shared dict for writing values
    # and passing values. We cannot put the build.meta['amis']
    # here since the Manager does allow mutating nested lists.
    manager = Manager()
    shared_meta = manager.dict({
        "source_region": source_region,
        "source_image": source_image,
        "args": args,
    })

    # Create the workers to do the work.
    workers = []
    for region in region_list:
        log.info(f"starting ore worker for {region}")
        p = Process(target=_replicate_worker, args=(region, shared_meta))
        workers.append(p)
        p.start()

    # wait for the workers to come back
    log.info("waiting for ore workers to complete")
    for worker in workers:
        worker.join()

    # check on the progress
    failed_regions = []
    for region in region_list:
        ami_data = shared_meta.get(region, None)
        if isinstance(ami_data, Exception):
            log.critical(f" {region} failed with exception: {ami_data}")
            failed_regions.append(region)
        elif not ami_data:
            log.critical(f" {region} failed to upload")
            failed_regions.append(region)
        else:
            log.info(f" {region} uploaded successfully")
            build.meta['amis'].extend(ami_data)
            build.meta_write()

    if len(failed_regions) > 0:
        raise Exception(f"failed upload in regions {failed_regions}")


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
