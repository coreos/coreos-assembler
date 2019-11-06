import boto3
from cosalib.cmdlib import (
    retry_stop,
    retry_boto_exception,
    retry_callback
)
from tenacity import retry


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def deregister_ami(ami_id, region):
    print(f"AWS: deregistering AMI {ami_id} in {region}")
    ec2 = boto3.client('ec2', region_name=region)
    ec2.deregister_image(ImageId=ami_id)


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def delete_snapshot(snap_id, region):
    print(f"AWS: removing snapshot {snap_id} in {region}")
    ec2 = boto3.client('ec2', region_name=region)
    ec2.delete_snapshot(SnapshotId=snap_id)
