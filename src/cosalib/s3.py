import boto3

from botocore.exceptions import ClientError
from cosalib.cmdlib import (
    retry_stop,
    retry_boto_exception,
    retry_callback
)
from tenacity import retry


class S3(object):
    def __init__(self):
        self.client = boto3.client('s3')

    def download_file(self, bucket, key, dest):
        self.client.download_file(bucket, key, dest,
            Config=boto3.s3.transfer.TransferConfig(num_download_attempts=5))

    @retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
    def head_bucket(self, bucket):
        self.client.head_bucket(Bucket=bucket)

    @retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
    def head_object(self, bucket, key):
        try:
            self.client.head_object(Bucket=bucket, Key=key)
        except ClientError as e:
            if e.response['Error']['Code'] == '404':
                return False
            raise e
        return True

    @retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
    def list_objects(self, bucket, prefix, delimiter="/", result_key='Contents'):
        kwargs = {
            'Bucket': bucket,
            'Delimiter': delimiter,
            'Prefix': prefix,
        }
        isTruncated = True
        while isTruncated:
            batch = self.client.list_objects_v2(**kwargs)
            yield from batch.get(result_key) or []
            kwargs['ContinuationToken'] = batch.get('NextContinuationToken')
            isTruncated = batch['IsTruncated']

    @retry(stop=retry_stop, retry=retry_boto_exception, before_sleep=retry_callback)
    def delete_object(self, bucket, key):
        sub_objects = list(self.list_objects(bucket, key))
        if sub_objects != []:
            print("S3: deleting {sub_objects}")
            self.client.delete_objects(Bucket=bucket, Delete=sub_objects)
        self.client.delete_object(Bucket=bucket, Key=key)
