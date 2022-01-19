import subprocess
import logging as log
import json
import sys
from cosalib.cmdlib import run_verbose
from tenacity import (
    retry,
    stop_after_attempt
)


def remove_aliyun_image(aliyun_id, region):
    print(f"aliyun: removing image {aliyun_id} in {region}")
    try:
        run_verbose([
            'ore',
            'aliyun', '--log-level', 'debug', 'delete-image',
            '--id', aliyun_id,
            '--force'])
    except SystemExit:
        raise Exception("Failed to remove image")


@retry(reraise=True, stop=stop_after_attempt(3))
def aliyun_run_ore_replicate(build, args):
    build.refresh_meta()
    aliyun_img_data = build.meta.get('aliyun', [])
    if len(aliyun_img_data) < 1:
        raise SystemExit(("buildmeta doesn't contain source images. "
                         "Run buildextend-aliyun first"))

    if not args.region:
        args.region = subprocess.check_output([
            'ore', f'--config-file={args.config}' if args.config else '',
            'aliyun', 'list-regions'
        ]).decode().strip().split()
        log.info(("default: replicating to all regions. If this is not "
                 " desirable, use '--regions'"))

    log.info("replicating to regions: %s", args.region)

    # only replicate to regions that don't already exist
    existing_regions = [item['name'] for item in aliyun_img_data]
    duplicates = list(set(args.region).intersection(existing_regions))
    if len(duplicates) > 0:
        print((f"Images already exist in {duplicates} region(s)"
               ", skipping listed region(s)..."))
    region_list = list(set(args.region) - set(duplicates))
    if len(region_list) == 0:
        print("no new regions detected")
        sys.exit(0)

    source_image = aliyun_img_data[0]['id']
    source_region = aliyun_img_data[0]['name']
    upload_name = f"{build.build_name}-{build.build_id}"

    ore_args = [
        'ore',
        '--log-level', args.log_level,
        'aliyun', 'copy-image',
        '--name', upload_name,
        '--description', f'{build.summary} {build.build_id}',
        '--image', source_image,
        '--region', source_region,
        '--wait-for-ready'
    ]

    if args.config:
        ore_args.extend(['--config-file', args.config])

    upload_failed_in_region = None

    # Copy the image to all regions. We'll then go mark
    # them public afterwards since it takes some time
    # for each image to show up in each region.
    for upload_region in region_list:
        region_ore_args = ore_args.copy() + [upload_region]
        print("+ {}".format(subprocess.list2cmdline(region_ore_args)))
        try:
            ore_data = json.loads(subprocess.check_output(region_ore_args))
        except subprocess.CalledProcessError:
            upload_failed_in_region = upload_region
            break

        aliyun_img_data.extend([
            {
                'name': region,
                'id': val
            } for region, val in ore_data.items()
        ])

    build.meta['aliyun'] = aliyun_img_data
    build.meta_write()

    # we've successfully replicated to *some* of the regions, so exit early.
    # if `cosa aliyun-replicate` is ran again with the same arguments, it will
    # retry the failed regions and then proceed to mark them public if
    # requested
    if upload_failed_in_region is not None:
        raise Exception(f"Upload failed in {upload_failed_in_region} region")

    # all images have been uploaded, so we can try to mark them public
    if args.public:
        make_public(build, args)


# a dedicated function for marking images public
def make_public(build, args):
    build.refresh_meta()
    aliyun_img_data = build.meta.get('aliyun', [])

    make_public_args = [
        'ore',
        '--log-level', args.log_level,
        'aliyun', 'visibility', '--public'
    ]

    if args.config:
        make_public_args.extend(['--config-file', args.config])

    # build out a list of region:image pairs to pass to `ore aliyun visibility`
    region_image_pairs = []
    for entry in aliyun_img_data:
        # id == image id, name == region id
        image_id = entry['id']
        region_id = entry['name']

        region_image_pairs.append(f"{region_id}:{image_id}")

    make_public_copy = make_public_args.copy() + region_image_pairs
    print("+ {}".format(subprocess.list2cmdline(make_public_copy)))
    try:
        subprocess.check_call(make_public_copy)
    except subprocess.CalledProcessError:
        raise Exception("Unable to mark all the desired images as public")


@retry(reraise=True, stop=stop_after_attempt(3))
def aliyun_run_ore(build, args):
    build.refresh_meta()
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])

    if args.force:
        ore_args.extend(['--force'])

    region = "us-west-1"
    if args.region is not None:
        region = args.region[0]

    upload_name = f"{build.build_name}-{build.build_id}"
    if args.name_suffix:
        upload_name = f"{build.build_name}-{args.name_suffix}-{build.build_id}"

    if args.bucket is None:
        raise Exception("Must supply OSS bucket when uploading")

    ore_args.extend([
        f'--config-file={args.config}' if args.config else '',
        'aliyun', 'create-image',
        '--region', region,
        '--bucket', args.bucket,
        '--name', upload_name,
        '--file', f"{build.image_path}",
        '--description', f'{build.summary} {build.build_id}',
        '--architecture', build.basearch,
        '--disk-size-inspect'
    ])

    print(ore_args)
    try:
        # convert the binary output to string and remove trailing white space
        ore_data = subprocess.check_output(ore_args).decode('utf-8').strip()
    except subprocess.CalledProcessError:
        raise Exception(f'Failed to create image in {region}')

    build.meta['aliyun'] = [{
        'name': region,
        'id': ore_data
    }]
    build.meta_write()

    if args.public:
        make_public(build, args)


def aliyun_cli(parser):
    parser.add_argument("--bucket", help="OSS Bucket")
    parser.add_argument("--name-suffix", help="Suffix for uploaded image name")
    parser.add_argument("--public", action="store_true", help="Mark images as publicly available")
    return parser
