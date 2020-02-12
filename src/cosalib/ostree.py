# Copyright 2018-2020 Red Hat, Inc
# Licensed under the new-BSD license (http://www.opensource.org/licenses/bsd-license.php)

import gi
import glob
import json
import logging as log
import os
import tarfile

gi.require_version('OSTree', '1.0')
from gi.repository import (
    Gio,
    OSTree
)
from cosalib.build import _Build
from cosalib.cmdlib import run_verbose

DEFAULT_OSTREE_META_SIZE_PERCENT = 5
DEFAULT_OSTREE_PADDING_PERCENT = 35
DEFAULT_OSTREE_INODE_SIZE = 512
DEFAULT_OSTREE_BLOCK_SIZE = 4069


def estimate_tree_size(repo, ref,
                       add_percent=DEFAULT_OSTREE_PADDING_PERCENT,
                       blksize=DEFAULT_OSTREE_BLOCK_SIZE,
                       isize=DEFAULT_OSTREE_INODE_SIZE,
                       metadata_overhead_percent=DEFAULT_OSTREE_META_SIZE_PERCENT,
                       ):
    """
    Get the size of the OSTree.
    """
    log.debug(f"Estimating tree size of {ref} at {repo}")
    r = OSTree.Repo.new(Gio.File.new_for_path(repo))
    r.open(None)

    [_, rev] = r.resolve_rev(ref, False)
    [_, reachable] = r.traverse_commit(rev, 0, None)
    n_meta = 0
    blks_meta = 0
    n_regfiles = 0
    blks_regfiles = 0
    n_symlinks = 0
    blks_symlinks = 0
    for k, v in reachable.items():
        csum, objtype = k.unpack()
        if objtype == OSTree.ObjectType.FILE:
            [_, _, finfo, _] = r.load_file(csum, None)
            if finfo.get_file_type() == Gio.FileType.REGULAR:
                n_regfiles += 1
                sz = finfo.get_size()
                blks_regfiles += (sz // blksize) + 1
            else:
                n_symlinks += 1
                sz = len(finfo.get_symlink_target())
                blks_symlinks += (sz // blksize) + 1
        else:
            [_, sz] = r.query_object_storage_size(objtype, csum, None)
            n_meta += 1
            blks_meta += (sz // blksize) + 1

    mb = 1024 * 1024
    blks_per_mb = mb // blksize
    total_data_mb = (
        blks_meta + blks_regfiles + blks_symlinks) // blks_per_mb
    n_inodes = n_meta + n_regfiles + n_symlinks
    total_inode_mb = 1 + ((n_inodes * isize) // mb)
    total_mb = total_data_mb + total_inode_mb
    add_percent = metadata_overhead_percent + add_percent
    add_percent_modifier = (100.0 + add_percent) / 100.0
    estimate_mb = int(total_mb * add_percent_modifier) + 1
    return {
        'meta': {
            'count': n_meta,
            'blocks': blks_meta,
        },
        'regfiles': {
            'count': n_regfiles,
            'blocks': blks_regfiles},
        'symlinks': {
            'count': n_symlinks,
            'blocks': blks_symlinks
        },
        'inodes': {
            'count': n_inodes,
            'mb': total_inode_mb,
        },
        'estimate-mb': {
            'base': total_mb,
            'final': estimate_mb
        },
    }


class OSTreeException(Exception):
    pass


class _BuildOSTree():
    """
    BuildOSTree interacts with OS Trees written to <dir>/tmp/repo from the
    CoreOS Assembler command "cosa build."

    The core consumer is cosalib.metal.MetalVariant.
    """
    def __init__(self, build):
        self._build = build
        if not isinstance(build, _Build):
            raise OSTreeException("BuildOStree expects a _Build Object")

        self._ostree_repo = os.path.join(build.workdir, "tmp", "repo")
        if not os.path.exists(self._ostree_repo):
            raise OSTreeException(
                f"missing ostree repo at {self._ostree_repo}")

        # find the compose-*json file to discern the ref we expect
        try:
            os.chdir(os.path.join(build.workdir, "tmp"))
            compose_candidates = glob.glob("compose-*.json")
            if len(compose_candidates) > 1:
                raise Exception("found multiple compose json files")
            c_file = compose_candidates[0]
            log.info(f"Reading {os.getcwd()}/{c_file} for compose information")
            with open(c_file, 'r') as data:
                self._compose_data = json.load(data)
            log.info(f"Using OSTree ref '{self.ref_name}'")
        finally:
            os.chdir(build.workdir)

    @property
    def build(self):
        return self._build

    @property
    def ostree_repo(self):
        return self._ostree_repo

    @property
    def ref_name(self):
        return self._compose_data.get("ref",
                                      f"tmpref-{self.build.build_name}")

    @property
    def ostree_repo_size(self):
        ors = getattr(self, "_ostree_repo_size", None)
        if ors is None:
            self._ostree_repo_size = estimate_tree_size(
                self.ostree_repo, self.ref_name)
        return self._ostree_repo_size

    @property
    def ostree_repo_estimated_size(self):
        return self.ostree_repo_size.get("estimated-mb", {}).get("final", 4096)

    @property
    def ostree_tarball_meta(self):
        return self.meta.get("images", {}).get("ostree", {})

    @property
    def ostree_tarball_path(self):
        return self.ostree_meta.get("path")

    @property
    def ostree_tarball_sha256(self):
        return self.ostree_meta.get("sha256")

    @property
    def ostree_size(self):
        return self.ostree_meta.get("size")

    def need_tree(self):
        log.info("Checking for OSTree")
        repo_path = self.ostree_repo
        create = False
        try:
            _ = self.ostree_repo
        except OSTreeException:
            log.debugf(f"OSTree is missing, creating new one at {repo_path}")
            create = True

        if not create:
            return

        if self.ostree_path is None:
            raise OSTreeException(f"no ostree in meta.json")

        with tarfile.open(self.ostree_path, 'r:*') as tf:
            log.info(f"extracting {self.ostree_path} to {self.ostree_repo}")
            tf.extractall(path=self.ostree_repo, numeric_owner=True)

    def create_ref(self):
        ref = getattr(self, "_ostree_ref", self.ref_name)
        if self.meta.get('ostree-commit'):
            log.debug("ostree has been commited, skipping ref creation")
            return

        log.debug("found temp ostree ref, creating commit")
        # create the ref
        run_verbose([
            "ostree", "refs",
            f"--repo={self.ostree_repo}",
            self.ostree_commit,
            "--create", ref
        ])
        new_ref = self.get_rev(use_ref=ref)
        log.info(f"mapped '{ref}' to '{new_ref}'")
        self.meta['ostree-commit'] = new_ref
        self.meta.write()

    def get_rev(self, use_ref=None):
        """
        Get the OSTree ref based on the name.
        """
        cmd = [
            "ostree", "rev-parse",
            f"--repo={self.ostree_repo}",
            use_ref
        ]
        rev = run_verbose(cmd, capture_output=True, check=True).stdout
        return rev.decode("utf-8").strip()
