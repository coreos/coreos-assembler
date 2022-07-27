from cosalib.cmdlib import runcmd


def create(repo, tag, imageTags):
    '''
    Create manifest list
    @imageTags list image tags
    @param repo str registry repository
    @param tag str manifest tag
    '''
    cmd = ["podman", "manifest", "create", f"{repo}:{tag}"]
    runcmd(cmd)
    for imgtag in imageTags:
        cmd = ["podman", "manifest", "add",
               f"{repo}:{tag}", f"docker://{repo}:{imgtag}"]
        runcmd(cmd)


def push(repo, tag, v2s2=None):
    '''
    Push manifest to registry
    @param repo str registry repository
    @param tag str manifest tag
    @param v2s2 boolean use force v2s2
    '''
    cmd = ["podman", "manifest",
           "push", "--all", f"{repo}:{tag}"]
    if v2s2:
        # `--remove-signatures -f v2s2` is a workaround for when you try
        # to create a manifest with 2 different mediaType. It seems to be
        # a Quay issue.
        cmd.extend(["--remove-signatures", "-f", "v2s2"])
    runcmd(cmd)
