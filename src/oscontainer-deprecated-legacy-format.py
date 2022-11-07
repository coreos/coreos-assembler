#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in the entrypoint for unbuffered output
#
# An "oscontainer" is an ostree (archive) repository stuck inside
# a Docker/OCI container at /srv/repo.  For more information,
# see https://github.com/openshift/pivot
#
# This command manipulates those images.

import gi

gi.require_version('OSTree', '1.0')
gi.require_version('RpmOstree', '1.0')

from gi.repository import GLib, Gio, OSTree, RpmOstree

import argparse
import json
import os
import shutil
import subprocess
from cosalib import cmdlib
from cosalib.buildah import (
    buildah_base_args
)

OSCONTAINER_COMMIT_LABEL = 'com.coreos.ostree-commit'


def run_get_json(args):
    return json.loads(subprocess.check_output(args))


def run_get_string(args):
    return subprocess.check_output(args, encoding='UTF-8').strip()


def run_verbose(args, **kwargs):
    print("+ {}".format(subprocess.list2cmdline(args)))
    subprocess.check_call(args, **kwargs)


def find_commit_from_oscontainer(repo):
    """Given an ostree repo, find exactly one commit object in it"""
    o = subprocess.check_output(['find', repo + '/objects', '-name', '*.commit'], encoding='UTF-8').strip().split('\n')
    if len(o) > 1:
        raise SystemExit(f"Multiple commit objects found in {repo}")
    d, n = os.path.split(o[0])
    return os.path.basename(d) + n.split('.')[0]


# Given a container reference, pull the latest version, then extract the ostree
# repo a new directory dest/repo.
def oscontainer_extract(containers_storage, tmpdir, src, dest,
                        tls_verify=True, ref=None, cert_dir="",
                        authfile=""):
    dest = os.path.realpath(dest)
    subprocess.check_call(["ostree", "--repo=" + dest, "refs"])

    # FIXME: Today we use skopeo in a hacky way for this.  What we
    # really want is the equivalent of `oc image extract` as part of
    # podman or skopeo.
    cmd = ['skopeo']
    # See similar message in oscontainer_build.
    if tmpdir is not None:
        os.environ['TMPDIR'] = tmpdir

    if not tls_verify:
        cmd.append('--tls-verify=false')

    if authfile != "":
        cmd.append("--authfile={}".format(authfile))
    if cert_dir != "":
        cmd.append("--cert-dir={}".format(cert_dir))
    tmp_tarball = tmpdir + '/container.tar'
    cmd += ['copy', "docker://" + src, 'docker-archive://' + tmp_tarball]
    run_verbose(cmd)
    run_verbose(['tar', 'xf', tmp_tarball], cwd=tmpdir)
    os.unlink(tmp_tarball)
    # This is a brutal hack to extract all the layers; we don't even bother with ordering
    # because we know we're not removing anything in higher layers.
    subprocess.check_call(['find', '-name', '*.tar', '-exec', 'tar', 'xUf', '{}', ';'], cwd=tmpdir)
    # Some files/directories aren't writable, and this will cause permission errors
    subprocess.check_call(['find', '!', '-perm', '-u+w', '-exec', 'chmod', 'u+w', '{}', ';'], cwd=tmpdir)

    repo = tmpdir + '/srv/repo'
    commit = find_commit_from_oscontainer(repo)
    print(f"commit: {commit}")
    run_verbose(["ostree", "--repo=" + dest, "pull-local", repo, commit])
    if ref is not None:
        run_verbose([
            "ostree", "--repo=" + dest, "refs", '--create=' + ref, commit])


# Given an OSTree repository at src (and exactly one ref) generate an
# oscontainer with it.
def oscontainer_build(containers_storage, tmpdir, src, ref, image_name_and_tag,
                      base_image, push=False, tls_verify=True, pushformat=None,
                      add_directories=[], cert_dir="", authfile="", digestfile=None,
                      display_name=None, labeled_pkgs=[]):
    r = OSTree.Repo.new(Gio.File.new_for_path(src))
    r.open(None)

    [_, rev] = r.resolve_rev(ref, True)
    if ref != rev:
        print("Resolved {} = {}".format(ref, rev))
    [_, ostree_commit, _] = r.load_commit(rev)
    ostree_commitmeta = ostree_commit.get_child_value(0)
    versionv = ostree_commitmeta.lookup_value(
        "version", GLib.VariantType.new("s"))
    if versionv:
        ostree_version = versionv.get_string()
    else:
        ostree_version = None

    buildah_base_argv = buildah_base_args(containers_storage)

    # In general, we just stick with the default tmpdir set up. But if a
    # workdir is provided, then we want to be sure that all the heavy I/O work
    # that happens stays in there since e.g. we might be inside a tiny supermin
    # appliance.
    if tmpdir is not None:
        os.environ['TMPDIR'] = tmpdir

    bid = run_get_string(buildah_base_argv + ['from', base_image])
    mnt = run_get_string(buildah_base_argv + ['mount', bid])
    try:
        dest_repo = os.path.join(mnt, 'srv/repo')
        subprocess.check_call(['mkdir', '-p', dest_repo])
        subprocess.check_call([
            "ostree", "--repo=" + dest_repo, "init", "--mode=archive"])
        # Note that oscontainers don't have refs; we also disable fsync
        # because the repo will be put into a container image and the build
        # process should handle its own fsync (or choose not to).
        print("Copying ostree commit into container: {} ...".format(rev))
        run_verbose(["ostree", "--repo=" + dest_repo, "pull-local", "--disable-fsync", src, rev])

        for d in add_directories:
            with os.scandir(d) as it:
                for entry in it:
                    dest = os.path.join(mnt, entry.name)
                    subprocess.check_call(['/usr/lib/coreos-assembler/cp-reflink', entry.path, dest])
                print(f"Copied in content from: {d}")

        # We use /noentry to trick `podman create` into not erroring out
        # on a container with no cmd/entrypoint.  It won't actually be run.
        config = ['--entrypoint', '["/noentry"]',
                  '-l', OSCONTAINER_COMMIT_LABEL + '=' + rev]
        if ostree_version is not None:
            config += ['-l', 'version=' + ostree_version]

        base_pkgs = RpmOstree.db_query_all(r, rev, None)
        for pkg in base_pkgs:
            name = pkg.get_name()
            if name in labeled_pkgs:
                config += ['-l', f"com.coreos.rpm.{name}={pkg.get_evr()}.{pkg.get_arch()}"]

        # Generate pkglist.txt in to the oscontainer at /
        pkg_list_dest = os.path.join(mnt, 'pkglist.txt')
        # should already be sorted, but just re-sort to be sure
        nevras = sorted([pkg.get_nevra() for pkg in base_pkgs])
        with open(pkg_list_dest, 'w') as f:
            for nevra in nevras:
                f.write(nevra)
                f.write('\n')

        meta = {}
        builddir = None
        if os.path.isfile('builds/builds.json'):
            with open('builds/builds.json') as fb:
                builds = json.load(fb)['builds']
            latest_build = builds[0]['id']
            arch = cmdlib.get_basearch()
            builddir = f"builds/{latest_build}/{arch}"
            metapath = f"{builddir}/meta.json"
            with open(metapath) as f:
                meta = json.load(f)
            rhcos_commit = meta['coreos-assembler.container-config-git']['commit']
            imagegit = meta.get('coreos-assembler.container-image-git')
            if imagegit is not None:
                cosa_commit = imagegit['commit']
                config += ['-l', f"com.coreos.coreos-assembler-commit={cosa_commit}"]
            config += ['-l', f"com.coreos.redhat-coreos-commit={rhcos_commit}"]

        if 'extensions' in meta:
            tarball = os.path.abspath(os.path.join(builddir, meta['extensions']['path']))
            dest_dir = os.path.join(mnt, 'extensions')
            os.makedirs(dest_dir, exist_ok=True)
            run_verbose(["tar", "-xf", tarball], cwd=dest_dir)

            with open(os.path.join(dest_dir, 'extensions.json')) as f:
                extensions = json.load(f)

            extensions_label = ';'.join([ext for (ext, obj) in extensions['extensions'].items()
                                         if obj.get('kind', 'os-extension') == 'os-extension'])
            config += ['-l', f"com.coreos.os-extensions={extensions_label}"]

            for pkgname in meta['extensions']['manifest']:
                if pkgname in labeled_pkgs:
                    evra = meta['extensions']['manifest'][pkgname]
                    config += ['-l', f"com.coreos.rpm.{pkgname}={evra}"]

        if display_name is not None:
            config += ['-l', 'io.openshift.build.version-display-names=machine-os=' + display_name,
                       '-l', 'io.openshift.build.versions=machine-os=' + ostree_version]
        run_verbose(buildah_base_argv + ['config'] + config + [bid])
        print("Committing container...")
        iid = run_get_string(buildah_base_argv + ['commit', bid, image_name_and_tag])
        print("{} {}".format(image_name_and_tag, iid))
    finally:
        subprocess.call(buildah_base_argv + ['umount', bid], stdout=subprocess.DEVNULL)
        subprocess.call(buildah_base_argv + ['rm', bid], stdout=subprocess.DEVNULL)

    if push:
        print("Pushing container")
        podCmd = buildah_base_argv + ['push']
        if not tls_verify:
            tls_arg = '--tls-verify=false'
        else:
            tls_arg = '--tls-verify'
        podCmd.append(tls_arg)

        if authfile != "":
            podCmd.append("--authfile={}".format(authfile))

        if cert_dir != "":
            podCmd.append("--cert-dir={}".format(cert_dir))

        if digestfile is not None:
            podCmd.append(f'--digestfile={digestfile}')

        if pushformat is not None:
            podCmd.append(f'--format={pushformat}')

        podCmd.append(image_name_and_tag)

        run_verbose(podCmd)
    elif digestfile is not None:
        inspect = run_get_json(buildah_base_argv + ['inspect', image_name_and_tag])[0]
        with open(digestfile, 'w') as f:
            f.write(inspect['Digest'])


def main():
    # Parse args and dispatch
    parser = argparse.ArgumentParser()
    parser.add_argument("--workdir", help="Temporary working directory")
    parser.add_argument("--disable-tls-verify",
                        help="Disable TLS for pushes and pulls",
                        default=(True if os.environ.get("DISABLE_TLS_VERIFICATION", False) else False),
                        action="store_true")
    parser.add_argument("--cert-dir", help="Extra certificate directories",
                        default=os.environ.get("OSCONTAINER_CERT_DIR", ''))
    parser.add_argument("--authfile", help="Path to authentication file",
                        action="store",
                        default=os.environ.get("REGISTRY_AUTH_FILE", ''))
    subparsers = parser.add_subparsers(dest='action')
    parser_extract = subparsers.add_parser(
        'extract', help='Extract an oscontainer')
    parser_extract.add_argument("src", help="Image reference")
    parser_extract.add_argument("dest", help="Destination directory")
    parser_extract.add_argument("--ref", help="Also set an ostree ref")
    parser_build = subparsers.add_parser('build', help='Build an oscontainer')
    parser_build.add_argument(
        "--from",
        help="Base image (default 'scratch')",
        default='scratch')
    parser_build.add_argument("src", help="OSTree repository")
    parser_build.add_argument("rev", help="OSTree ref (or revision)")
    parser_build.add_argument("name", help="Image name")
    parser_build.add_argument("--display-name", help="Name used for an OpenShift component")
    parser_build.add_argument("--add-directory", help="Copy in all content from referenced directory DIR",
                              metavar='DIR', action='append', default=[])
    parser_build.add_argument("--labeled-packages", help="Packages whose NEVRAs are included as labels on the image")
    # For now we forcibly override to v2s2 https://bugzilla.redhat.com/show_bug.cgi?id=2058421
    parser_build.add_argument("--format", help="Pass through push format to buildah", default="v2s2")
    parser_build.add_argument(
        "--digestfile",
        help="Write image digest to file",
        action='store',
        metavar='FILE')
    parser_build.add_argument(
        "--push",
        help="Push to registry",
        action='store_true')
    args = parser.parse_args()

    labeled_pkgs = []
    if args.labeled_packages is not None:
        labeled_pkgs = args.labeled_packages.split()

    containers_storage = None
    tmpdir = None
    if args.workdir is not None:
        containers_storage = os.path.join(args.workdir, 'containers-storage')
        if os.path.exists(containers_storage):
            shutil.rmtree(containers_storage)
        tmpdir = os.path.join(args.workdir, 'tmp')
        if os.path.exists(tmpdir):
            shutil.rmtree(tmpdir)
        os.makedirs(tmpdir)

    try:
        if args.action == 'extract':
            oscontainer_extract(
                containers_storage, tmpdir, args.src, args.dest,
                tls_verify=not args.disable_tls_verify,
                cert_dir=args.cert_dir,
                ref=args.ref,
                authfile=args.authfile)
        elif args.action == 'build':
            oscontainer_build(
                containers_storage, tmpdir, args.src, args.rev, args.name,
                getattr(args, 'from'),
                display_name=args.display_name,
                digestfile=args.digestfile,
                add_directories=args.add_directory,
                push=args.push,
                pushformat=args.format,
                tls_verify=not args.disable_tls_verify,
                cert_dir=args.cert_dir,
                authfile=args.authfile,
                labeled_pkgs=labeled_pkgs)
    finally:
        if containers_storage is not None and os.path.isdir(containers_storage):
            shutil.rmtree(containers_storage)


if __name__ == '__main__':
    main()
