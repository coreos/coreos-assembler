from cosalib.utils import run_cmd


def create(repo, tag, imageTags):
    '''
    Create manifest list
    @imageTags list image tags
    @param repo str registry repository
    @param tag str manifest tag
    '''
    run_cmd(f"podman manifest create {repo}:{tag}")
    for imgtag in imageTags:
        run_cmd(f"podman manifest add {repo}:{tag} docker://{repo}:{imgtag}")


def push(repo, tag, v2s2=None):
    '''
    Push manifest to registry
    @param repo str registry repository
    @param tag str manifest tag
    @param v2s2 boolean use force v2s2
    '''
    # ` --remove-signatures -f v2s2` is an workaround for when you try to create a manifest with 2 different mediaType. It seems
    # an Quay issue
    if v2s2:
        run_cmd(f"podman manifest push --all {repo}:{tag} docker://{repo}:{tag}  --remove-signatures -f v2s2")
    else:
        run_cmd(f"podman manifest push --all {repo}:{tag} docker://{repo}:{tag}")
