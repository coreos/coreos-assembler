import os

# https://access.redhat.com/documentation/en-us/openshift_container_platform/4.1/html/builds/custom-builds-buildah
NESTED_BUILD_ARGS = ['--storage-driver', 'vfs']


def buildah_base_args(containers_storage=None):
    buildah_base_argv = ['buildah']
    if containers_storage is not None:
        buildah_base_argv.append(f"--root={containers_storage}")
    if os.environ.get('container') is not None:
        print("Using nested container mode due to container environment variable")
        buildah_base_argv.extend(NESTED_BUILD_ARGS)
    else:
        print("Skipping nested container mode")
    return buildah_base_argv
