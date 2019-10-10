import boto3

from botocore.exceptions import ClientError
from cosalib.cmdlib import (
    retry_stop,
    retry_boto_exception,
    retry_callback
)
from tenacity import retry


S3 = boto3.client('s3')
S3CONFIG = boto3.s3.transfer.TransferConfig(
    num_download_attempts=5
)


def download_file(bucket, key, dest):
    S3.download_file(bucket, key, dest, Config=S3CONFIG)


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def head_bucket(bucket):
    S3.head_bucket(Bucket=bucket)


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def head_object(bucket, key):
    try:
        S3.head_object(Bucket=bucket, Key=key)
    except ClientError as e:
        if e.response['Error']['Code'] == '404':
            return False
        raise e
    return True


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def list_objects(bucket, prefix, delimiter="/", result_key='Contents'):
    kwargs = {
        'Bucket': bucket,
        'Delimiter': delimiter,
        'Prefix': prefix,
    }
    isTruncated = True
    while isTruncated:
        batch = S3.list_objects_v2(**kwargs)
        yield from batch.get(result_key) or []
        kwargs['ContinuationToken'] = batch.get('NextContinuationToken')
        isTruncated = batch['IsTruncated']


@retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
def delete_object(bucket, key):
    sub_objects = list(list_objects(bucket, key))
    if sub_objects != []:
        print("S3: deleting {sub_objects}")
        S3.delete_objects(Bucket=bucket, Delete=sub_objects)
    S3.delete_object(Bucket=bucket, Key=key)
