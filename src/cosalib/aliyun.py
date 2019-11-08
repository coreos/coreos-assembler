from cosalib.cmdlib import run_verbose


def remove_aliyun_image(aliyun_id, region):
    print(f"aliyun: removing image {aliyun_id} in {region}")
    try:
        run_verbose(['ore', 'aliyun', '--log-level', 'debug', 'delete-image',
                     '--id', aliyun_id,
                     '--force'])
    except SystemExit:
        raise Exception("Failed to remove image")
