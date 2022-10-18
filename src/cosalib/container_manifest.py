from cosalib.utils import runcmd


def create_local_container_manifest(repo, tag, images):
    '''
    Create local manifest list
    @param images list of image specifications (including transport)
    @param repo str registry repository
    @param tag str manifest tag
    '''
    cmd = ["podman", "manifest", "create", f"{repo}:{tag}"]
    runcmd(cmd)
    for image in images:
        cmd = ["podman", "manifest", "add", f"{repo}:{tag}", image]
        runcmd(cmd)


def delete_local_container_manifest(repo, tag):
    '''
    Delete local manifest list
    @param repo str registry repository
    @param tag str manifest tag
    '''
    cmd = ["podman", "manifest", "rm", f"{repo}:{tag}"]
    runcmd(cmd)


def push_container_manifest(repo, tags, digestfile, v2s2=False):
    '''
    Push manifest to registry
    @param repo str registry repository
    @param tags list of tags to push
    @param digestfile str write container digest to file
    @param v2s2 boolean use to force v2s2 format
    '''
    base_cmd = ["podman", "manifest", "push", "--all", f"{repo}:{tags[0]}"]
    if v2s2:
        # `--remove-signatures -f v2s2` is a workaround for when you try
        # to create a manifest with 2 different mediaType. It seems to be
        # a Quay issue.
        base_cmd.extend(["--remove-signatures", "-f", "v2s2"])
    if digestfile:
        base_cmd.extend([f"--digestfile={digestfile}"])
    runcmd(base_cmd + [f"{repo}:{tags[0]}"])
    for tag in tags[1:]:
        runcmd(base_cmd + [f"{repo}:{tag}"])


def create_and_push_container_manifest(repo, tags, images, v2s2, digestfile):
    '''
    Do it all! Create, Push, Cleanup
    @param repo str registry repository
    @param tags list of tags
    @param images list of image specifications (including transport)
    @param v2s2 boolean use to force v2s2 format
    @param digestfile str write container digest to file
    '''
    create_local_container_manifest(repo, tags[0], images)
    push_container_manifest(repo, tags, digestfile, v2s2)
    delete_local_container_manifest(repo, tags[0])
