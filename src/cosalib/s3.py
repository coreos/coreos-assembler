import boto3

from botocore.exceptions import ClientError, NoCredentialsError
from cosalib.cmdlib import (
    retry_stop,
    retry_stop_long,
    retry_wait_long,
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


@retry(stop=retry_stop_long, wait=retry_wait_long,
       retry=retry_boto_exception, before_sleep=retry_callback)
def s3_check_exists(s3_client, bucket, key, dry_run=False):
    print(f"Checking if bucket '{bucket}' has key '{key}'")
    try:
        s3_client.head_object(Bucket=bucket, Key=key)
    except ClientError as e:
        if e.response['Error']['Code'] == '404':
            return False
        raise e
    except NoCredentialsError as e:
        # It's reasonable to run without creds if doing a dry-run
        if dry_run:
            return False
        raise e
    return True


@retry(stop=retry_stop_long, wait=retry_wait_long,
       retry=retry_boto_exception, retry_error_callback=retry_callback)
def s3_copy(s3_client, src, bucket, key, max_age, acl, extra_args={}, dry_run=False):
    extra_args = dict(extra_args)
    if 'ContentType' not in extra_args:
        if key.endswith('.json'):
            extra_args['ContentType'] = 'application/json'
        elif key.endswith('.tar'):
            extra_args['ContentType'] = 'application/x-tar'
        elif key.endswith('.xz'):
            extra_args['ContentType'] = 'application/x-xz'
        elif key.endswith('.gz'):
            extra_args['ContentType'] = 'application/gzip'
        elif key.endswith('.iso'):
            extra_args['ContentType'] = 'application/x-iso9660-image'
        else:
            # use a standard MIME type for "binary blob" instead of the default
            # 'binary/octet-stream' AWS slaps on
            extra_args['ContentType'] = 'application/octet-stream'
    upload_args = {
        'CacheControl': f'max-age={max_age}',
        'ACL': acl
    }
    upload_args.update(extra_args)

    print((f"{'Would upload' if dry_run else 'Uploading'} {src} to "
           f"s3://{bucket}/{key} {extra_args if len(extra_args) else ''}"))

    if dry_run:
        return

    s3_client.upload_file(Filename=src, Bucket=bucket, Key=key, ExtraArgs=upload_args)
