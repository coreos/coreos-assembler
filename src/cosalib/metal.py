"""
Base Class for building Metal-type disks.
"""

# Copyright 2018-2020 Red Hat, Inc
# Licensed under the new-BSD license (http://www.opensource.org/licenses/bsd-license.php)

import distutils.util as dutil
import logging as log
import os
import os.path
import sys
import yaml

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.build import (
    _Build,
    BuildExistsError
)
from cosalib.ostree import _BuildOSTree
from cosalib.cmdlib import (
    get_basearch,
    run_verbose,
)


# For padded, disk is actual plus 513M
SIZE_PADDED = "513M"
# For estimated sizes, inflate the actual rootfs by this percent
SIZE_ESITMATED = 35
# rootfs defaults to being 'xfs'
DEFAULT_ROOTFS = "xfs"

# VARIANTS are the type of disks that can build built
# To define new variants add its definition below:
VARIANTS = {
    "qemu": {
        "arch": "any",
        "platform": "qemu",
        "format": "qcow2",
        "target_args": [
            "-drive",
            "if=virtio,id=target,format={format},file={outfile},cache=unsafe"
        ],
        "kargs": [
            "console=tty0 console={terminal},115200n8",
            "ignition.platform.id={platform}"
        ],
    },
    "metal": {
        "arch": "any",
        "platform": "metal",
        "format": "raw",
        "kargs": [
            "console=tty0 console={terminal},115200n8",
            "ignition.platform.id={platform}"
        ],
        "target_args": [
            "-drive",
            "if=virtio,id=target,format={format},file={outfile},cache=unsafe"
        ],
    },
    "dasd": {
        "arch": "s390x",
        "format":  "raw",
        "platform": "metal",
        "kargs": [
            "ignition.platform.id={platform}"
        ],
        "target_args": [
            "-drive",
            "if=virtio,id=target,format={format},file={outfile},cache=unsafe"
            "-device",
            ("virtio-blk-ccw,drive=target,physical_block_size=4096,"
             "logical_block_size=4096,scsi=off")
        ],
    }
}


ARCH_TERMINAL = {
    "x86_64": "ttyS0",
    "ppc64le": "hvc0",
    "aarch64": "ttyAMA0",
    "s390x": "ttysclp0",
}


def arch_terminal(arch):
    """
    Get the default terminal based on the architecture
    """
    if arch is None:
        arch = get_basearch()
    return ARCH_TERMINAL[arch]


class MetalImageError(Exception):
    """
    Base error for build issues.
    """
    pass


class MetalVariantImage(_Build, _BuildOSTree):
    """
    MetalVariantImage extends both the _Build and BuildOSTree classes.
    """

    def __init__(self, *args, **kwargs):
        """
        This takes all the regular _BuildClass arguments. In kwargs, the
        additional arguments are used:
            platform: the name of the image.
            format: the disk format type, e.g. raw
            arch: build for specific architecture
            force: force the build
            target_args: qemu specific arguments

        Alternatively, you can provide "variant=<variant>" and defaults will be
        used. Default to building "metal" disk targets.
        """
        self._image_name = None
        variant = kwargs.pop("variant", "metal")
        if variant:
            kwargs.update(VARIANTS.get(variant, {}))
        self.arch = kwargs.pop("arch", get_basearch)
        self.force = kwargs.get("force", False)
        self.format = kwargs.pop("format")
        self.kargs = kwargs.pop("kargs", [])
        self.platform = kwargs.pop("platform")
        self.target_args = kwargs.pop("target_args", [])

        # Load the _Build super class
        _Build.__init__(self, *args, **kwargs)
        # Load the _BuildOSTree super class.
        _BuildOSTree.__init__(self, self)

        # Read in the image.yaml and manifest.yaml files
        self.img_cfg = self.read_yaml_file("image.yaml")
        self.manifest = self.read_yaml_file("manifest.yaml")

        # sanity check the OSTree against the expected ref
        ref = self.manifest.get("ref")
        if ref and ref.replace("${arch}", self.basearch) != ref:
            raise MetalImageError(f"expected ref {ref} mismatches")

        # this is used in case the image has a different disk
        # name than the platform
        self.platform_image_name = kwargs.get(
            "platform_image_name", self.platform)

        # building dasd for ppc64el makes no sense
        if self.arch != "any" and self.arch != self.basearch:
            raise MetalImageError(
                f"{self.basearch} cannot be built for disk {variant}")

        # only support known-good architectures, eg. no sparks
        if self.basearch not in ["x86_64", "aarch64", "s390x", "ppc64le"]:
            raise MetalImageError(
                f"{self.basearch} is not support for metal-type disk images")

    def read_yaml_file(self, yaml_fname):
        """
        Get the contents of yaml_fname relative to the <dir>/src/config
        """
        fname = os.path.join(self.workdir, "src", "config", f"{yaml_fname}")
        if not os.path.exists(fname):
            raise Exception(f"failed to find file {fname}")
        data = None
        with open(fname, 'r') as f:
            data = yaml.safe_load(f.read())
        if not data:
            raise Exception(f"{yaml_fname}.yaml did not read in properly")
        return data

    @property
    def image_name(self):
        if self._image_name is not None:
            return self._image_name

        return (f'{self.build_name}-{self.build_id}'
                f'-{self.platform}.{self.basearch}.{self.format}')

    @property
    def rootfs_size(self):
        """
        Returns the rootfs size and the image size.
        """
        size = self.ostree_repo_estimated_size
        return f"{int(size * 1.35)}M"

    @property
    def disk_size(self):
        """
        Returns the size of the disk. This is the size of the
        rootfs with padding
        """
        size = self.img_cfg.get("size")
        if size:
            if str(size).upper().endswith("M"):
                return size
            else:
                return f"{size}G"
        return f"{self.rootfs_size + 513}M"

    @property
    def rootfs_type(self):
        rootfs = self.img_cfg.get("rootfs", DEFAULT_ROOTFS)
        rootfs_luks = str(self.img_cfg.get("luks_rootfs", False)).lower()
        if dutil.strtobool(rootfs_luks):
            rootfs = "luks"
            log.debug("rootfs will be housed in LUKS")
        return rootfs

    def get_disk_args(self, ref):
        """
        Generator for returning disk arguments.
        """
        tmod_cmd = run_verbose(
            [
                "ostree", f"--repo={self.ostree_repo}",
                "ls", ref, "/usr/lib/modules",
            ],
            check=True, capture_output=True
        ).stdout.decode('utf-8').splitlines()[-1]
        target_moduledir = tmod_cmd.split()[-1]
        log.info(f"Kernel Modules found at {target_moduledir}")

        kconfig = run_verbose(
            [
                "ostree", f"--repo={self.ostree_repo}",
                "cat", ref,
                f"{target_moduledir}/config"
            ],
            check=True, capture_output=True
        ).stdout.decode('utf-8')

        for l in kconfig.splitlines():
            if "CONFIG_FS_VERITY=y" in l:
                yield '--boot-verity'
                break

    def mk_image(self):
        """
        mk_image creates the final disk
        """
        self.create_ref()
        # get the root files system

        # Get the kernel args
        kargs = self.kargs
        kargs.extend(self.img_cfg.get("extra-kargs", []))
        kargs = [
            x.format(
                platform=self.platform,
                terminal=arch_terminal(self.basearch)
            ) for x in kargs
        ]
        log.debug(f"using '{kargs}' for image kernel cli")

        # Create the ref
        self.create_ref()

        # create the disk
        run_verbose([
            'qemu-img', 'create', '-f',
            self.format, self.image_path,
            self.disk_size
        ])

        # Format the command. 'runvm' is in cmdlib.sh.
        cmd = ['runvm']
        for arg in self.target_args:
            cmd.append(arg.format(format=self.format,
                                  outfile=self.image_path))

        cmd.extend([
            '--',
            '/usr/lib/coreos-assembler/create_disk.sh',
            '--buildid', self.build_id,
            '--disk', '/dev/vda',
            '--grub-script', '/usr/lib/coreos-assembler/grub.cfg',
            '--imgid', self.platform,
            '--kargs', f'\'"{" ".join(kargs)}"\'',
            '--osname', self.build_name,
            '--ostree-ref', self.ref_name,
            '--ostree-remote', self.img_cfg.get("ostree-remote", "NONE"),
            '--ostree-repo', self.ostree_repo,
            '--rootfs', self.rootfs_type,
            '--rootfs-size', self.rootfs_size,
            '--save-var-subdirs',
            self.img_cfg.get("subdirs-for-selabel-workaround", "NONE"),
        ])
        cmd.extend(self.get_disk_args(self.ref_name))

        # Ensure that Python None is represented as "NONE"
        for i in range(len(cmd)):
            cmd[i] = 'NONE' if cmd[i] is None else cmd[i]

        run_verbose(cmd, cmdlib_sh=True)

    def _build_artifacts(self, *args, **kwargs):
        if self.have_artifact and not self.force:
            raise BuildExistsError(
                f"{self.image_name} has already been built.")

        self.mk_image()
        imgs = self.meta.get("images", {})
        img_meta = self.get_artifact_meta()
        self._found_files[self.image_name] = img_meta
        imgs[self.platform] = img_meta
        self.meta_write()
