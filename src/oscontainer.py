#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in cmdlib.sh for unbuffered output
#
# An "oscontainer" is an ostree (archive) repository stuck inside
# a Docker/OCI container at /srv/repo.  For more information,
# see https://github.com/openshift/pivot
#
# This command manipulates those images.

import gi
gi.require_version('OSTree', '1.0')
from gi.repository import GLib, Gio, OSTree
import os,sys,json,shutil,argparse,subprocess,re,collections
import tempfile,hashlib,gzip

OSCONTAINER_COMMIT_LABEL = 'com.coreos.ostree-commit'

def run_get_json(args):
    return json.loads(subprocess.check_output(args))

def run_get_string(args):
    return subprocess.check_output(args, encoding='UTF-8').strip()

def run_verbose(args, **kwargs):
    print("+ {}".format(subprocess.list2cmdline(args)))
    subprocess.check_call(args, **kwargs)

# Given a container reference, pull the latest version, then extract the ostree
# repo a new directory dest/repo.
def oscontainer_extract(containers_storage, src, dest,
                        tls_verify=True, ref=None, cert_dir=""):
    dest = os.path.realpath(dest)
    subprocess.check_call(["ostree", "--repo="+dest, "refs"])
    rootarg = '--root='+containers_storage
    podCmd = ['podman', rootarg, 'pull']

    if not tls_verify:
        tls_arg = '--tls-verify=false'
    else:
        tls_arg = '--tls-verify'
    podCmd.append(tls_arg)

    if cert_dir != "":
        podCmd.append("--cert-dir={}".format(cert_dir))
    podCmd.append(src)

    run_verbose(podCmd)
    inspect = run_get_json(['podman', rootarg, 'inspect', src])[0]
    commit = inspect['Labels'].get(OSCONTAINER_COMMIT_LABEL)
    if commit is None:
        raise SystemExit("Failed to find label '{}'".format(OSCONTAINER_COMMIT_LABEL))
    iid = inspect['Id']
    print("Preparing to extract cid: {}".format(iid))
    # We're not actually going to run the container. The main thing `create` does
    # then for us is "materialize" the merged rootfs, so we can mount it.
    # In theory we shouldn't need --entrypoint=/enoent here, but
    # it works around a podman bug.
    cid = run_get_string(['podman', rootarg, 'create', '--entrypoint=/enoent', iid])
    mnt = run_get_string(['podman', rootarg, 'mount', cid])
    try:
        src_repo = os.path.join(mnt, 'srv/repo')
        run_verbose(["ostree", "--repo="+dest, "pull-local", src_repo, commit])
    finally:
        subprocess.call(['podman', rootarg, 'umount', cid])
    if args.ref is not None:
        run_verbose(["ostree", "--repo="+dest, "refs", '--create='+args.ref, commit])


# Given an OSTree repository at src (and exactly one ref) generate an oscontainer
# with it.
def oscontainer_build(containers_storage, src, ref, image_name_and_tag,
                      base_image, push=False, tls_verify=True, cert_dir="",
                      inspect_out=None):
    r = OSTree.Repo.new(Gio.File.new_for_path(src))
    r.open(None)

    [_, rev] = r.resolve_rev(ref, True)
    if ref != rev:
        print("Resolved {} = {}".format(ref, rev))
    [_, ostree_commit, _] = r.load_commit(rev)
    ostree_commitmeta = ostree_commit.get_child_value(0)
    versionv = ostree_commitmeta.lookup_value("version", GLib.VariantType.new("s"))
    if versionv:
        ostree_version = versionv.get_string()
    else:
        ostree_version = None

    rootarg = '--root='+containers_storage
    bid = run_get_string(['buildah', rootarg, 'from', base_image])
    mnt = run_get_string(['buildah', rootarg, 'mount', bid])
    try:
        dest_repo = os.path.join(mnt, 'srv/repo')
        subprocess.check_call(['mkdir', '-p', dest_repo])
        subprocess.check_call(["ostree", "--repo="+dest_repo, "init", "--mode=archive"])
        # Note that oscontainers don't have refs
        print("Copying ostree commit into container: {} ...".format(rev))
        run_verbose(["ostree", "--repo="+dest_repo, "pull-local", src, rev])

        # We use /noentry to trick `podman create` into not erroring out
        # on a container with no cmd/entrypoint.  It won't actually be run.
        config=['--entrypoint', '["/noentry"]',
                '-l', OSCONTAINER_COMMIT_LABEL+'='+rev]
        if ostree_version is not None:
            config += ['-l', 'version='+ostree_version]
        run_verbose(['buildah', rootarg, 'config'] + config + [bid])
        print("Committing container...")
        iid = run_get_string(['buildah', rootarg, 'commit', bid, image_name_and_tag])
        print("{} {}".format(image_name_and_tag, iid))
    finally:
        subprocess.call(['buildah', rootarg, 'umount', bid], stdout=subprocess.DEVNULL)
        subprocess.call(['buildah', rootarg, 'rm', bid], stdout=subprocess.DEVNULL)

    if push:
        print("Pushing container")
        podCmd = ['podman', rootarg, 'push']
        if not tls_verify:
            tls_arg = '--tls-verify=false'
        else:
            tls_arg = '--tls-verify'
        podCmd.append(tls_arg)

        if cert_dir != "":
            podCmd.append("--cert-dir={}".format(cert_dir))
        podCmd.append(image_name_and_tag)

        run_verbose(podCmd)
        inspect = run_get_json(['skopeo', 'inspect', "docker://"+image_name_and_tag])
    else:
        inspect = run_get_json(['podman', rootarg, 'inspect', image_name_and_tag])[0]
    if inspect_out is not None:
        with open(inspect_out, 'w') as f:
            json.dump(inspect, f)

# Parse args and dispatch
parser = argparse.ArgumentParser()
parser.add_argument("--workdir", help="Temporary working directory",
                    required=True)
parser.add_argument("--disable-tls-verify", help="Disable TLS for pushes and pulls",
                    action="store_true")
parser.add_argument("--cert-dir", help="Extra certificate directories",
                    default=os.environ.get("OSCONTAINER_CERT_DIR", ''))
subparsers = parser.add_subparsers(dest='action')
parser_extract = subparsers.add_parser('extract', help='Extract an oscontainer')
parser_extract.add_argument("src", help="Image reference")
parser_extract.add_argument("dest", help="Destination directory")
parser_extract.add_argument("--ref", help="Also set an ostree ref")
parser_build = subparsers.add_parser('build', help='Build an oscontainer')
parser_build.add_argument("--from", help="Base image (default 'scratch')", default='scratch')
parser_build.add_argument("src", help="OSTree repository")
parser_build.add_argument("rev", help="OSTree ref (or revision)")
parser_build.add_argument("name", help="Image name")
parser_build.add_argument("--inspect-out", help="Write image JSON to file",
                          action='store', metavar='FILE')
parser_build.add_argument("--push", help="Push to registry",
                          action='store_true')
args = parser.parse_args()

containers_storage = os.path.join(args.workdir, 'containers-storage')
if os.path.exists(containers_storage):
    shutil.rmtree(containers_storage)

if args.action == 'extract':
    oscontainer_extract(containers_storage, args.src, args.dest,
                        tls_verify=not args.disable_tls_verify,
                        cert_dir=args.cert_dir,
                        ref=args.ref)
elif args.action == 'build':
    oscontainer_build(containers_storage, args.src, args.rev, args.name,
                      getattr(args, 'from'),
                      inspect_out=args.inspect_out,
                      push=args.push,
                      tls_verify=not args.disable_tls_verify,
                      cert_dir=args.cert_dir)
