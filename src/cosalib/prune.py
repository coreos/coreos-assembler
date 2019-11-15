import collections
import json
import os

from cosalib.s3 import (
    head_object,
    list_objects,
    download_file,
    delete_object
)

from cosalib.aws import (
    deregister_ami,
    delete_snapshot
)

from cosalib.aliyun import remove_aliyun_image
from cosalib.gcp import remove_gcp_image
from cosalib.azure import remove_azure_image


Build = collections.namedtuple('Build', ['id', 'timestamp', 'images', 'arches'])


def get_unreferenced_s3_builds(active_build_set, bucket, prefix):
    """
    Scans s3 bucket and returns a list of build ID in the prefix

    :param active_build_set: list of known builds
    :type active_build_set: list
    """
    print(f"Looking for unreferenced builds in s3://{bucket}/{prefix}")
    s3_subdirs = list_objects(bucket, f"{prefix}/", result_key='CommonPrefixes')
    s3_matched = set()
    s3_unmatched = set()
    for prefixKey in s3_subdirs:
        subdir = prefixKey['Prefix']
        buildid = subdir.replace(prefix, '').strip("/")
        if buildid not in active_build_set:
            s3_unmatched.add(buildid)
        else:
            s3_matched.add(buildid)
    for buildid in active_build_set:
        if buildid not in s3_matched:
            print(f"WARNING: Failed to find build in S3: {buildid}")
    print(f"Found {len(s3_unmatched)} builds")
    return s3_unmatched


def fetch_build_meta(builds, buildid, arch, bucket, prefix):
    print(f"Looking for meta.json for '{buildid}'")
    build_dir = builds.get_build_dir(buildid, arch)

    # Fetch missing meta.json paths
    meta_json_path = os.path.join(build_dir, "meta.json")
    if not os.path.exists(meta_json_path):
        # Fetch it from s3
        os.makedirs(build_dir, exist_ok=True)
        s3_key = f"{prefix}/{buildid}/{arch}/meta.json"
        print(f"Fetching meta.json for '{buildid}' from s3://{bucket}/{prefix} to {meta_json_path}")
        head_result = head_object(bucket, s3_key)
        if head_result:
            print(f"Found s3 key at {s3_key}")
            download_file(bucket, s3_key, meta_json_path)
        else:
            print(f"Failed to find object at {s3_key}")
            return None

    buildmeta_path = os.path.join(meta_json_path)
    with open(buildmeta_path) as f:
        buildmeta = json.load(f)
        images = {
            'amis': buildmeta.get('amis') or [],
            'azure': buildmeta.get('azure') or [],
            'gcp': buildmeta.get('gcp') or [],
        }
        return Build(
            id=buildid,
            timestamp=buildmeta['coreos-assembler.build-timestamp'],
            images=images,
            arches=arch
        )


def delete_build(build, bucket, prefix, cloud_config, force=False):
    print(f"Deleting build {build.id}")
    errors = []
    # Unregister AMIs and snapshots
    for ami in build.images.get('amis', []):
        region_name = ami.get('name')
        ami_id = ami.get('hvm')
        snapshot_id = ami.get('snapshot')
        if ami_id and region_name:
            try:
                deregister_ami(ami_id, region=region_name)
            except Exception as e:
                errors.append(e)
        if snapshot_id and region_name:
            try:
                delete_snapshot(snapshot_id, region=region_name)
            except Exception as e:
                errors.append(e)

    aliyun = build.images.get('aliyun')
    if aliyun:
        region_name = aliyun.get('name')
        aliyun_id = aliyun.get('hvm')
        if region_name and aliyun_id:
            try:
                remove_aliyun_image(aliyun_id, region=region_name)
            except Exception as e:
                errors.append(e)

    azure = build.images.get('azure')
    if azure:
        image = azure.get('image')
        resource_group = cloud_config.get('azure', {}).get('resource-group')
        auth = cloud_config.get('azure', {}).get('auth')
        profile = cloud_config.get('azure', {}).get('profile')
        if image and resource_group and auth and profile:
            try:
                remove_azure_image(image, resource_group, auth, profile)
            except Exception as e:
                errors.append(e)

    gcp = build.images.get('gcp')
    if gcp:
        gcp_image = gcp.get('image')
        json_key = cloud_config.get('gcp', {}).get('json-key')
        project = cloud_config.get('gcp', {}).get('project')
        if gcp_image and json_key and project:
            try:
                remove_gcp_image(gcp_image, json_key, project)
            except Exception as e:
                errors.append(e)

    if len(errors) != 0:
        print(f"Found errors when removing build {build.id}:")
        for e in errors:
            print(e)
        if not force:
            raise Exception()

    # Delete s3 bucket
    print(f"Deleting key {prefix}{build.id} from bucket {bucket}")
    delete_object(bucket, f"{prefix}{str(build.id)}")
