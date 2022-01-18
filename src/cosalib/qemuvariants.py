"""
Provides a base abstration for building images.
"""

import logging as log
import os.path
import subprocess
import shutil
import sys

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.build import (
    _Build,
    BuildExistsError
)
from cosalib.cmdlib import (
    get_basearch,
    image_info,
    run_verbose,
    sha256sum_file
)


# BASEARCH is the current machine architecture
BASEARCH = get_basearch()

# Default flags for the creation of tarfiles
# The default flags were selected because:
#   -S: always create a sparse file
#   -c: create a new tarball
#   -h: derefence symlinks
# These flags were selected from prior commits for
# tarball creation.
DEFAULT_TAR_FLAGS = '-Sch'

# Variant are disk types that are derived from qemu images.
# To define new variants that use the QCOW2 disk image, simply,
# add its definition below:
VARIANTS = {
    "aws": {
        "image_format": "vmdk",
        "image_suffix": "vmdk",
        "platform": "aws",
        "convert_options": {
            '-o': 'adapter_type=lsilogic,subformat=streamOptimized,compat6'
        },
    },
    "aliyun": {
        "image_format": "qcow2",
        "platform": "aliyun",
    },
    "azure": {
        "image_format": "vpc",
        "image_suffix": "vhd",
        "platform": "azure",
        "convert_options": {
            '-o': 'force_size,subformat=fixed'
        },
    },
    "azurestack": {
        "image_format": "vpc",
        "image_suffix": "vhd",
        "platform": "azurestack",
        "convert_options": {
            '-o': 'force_size,subformat=fixed'
        },
    },
    "digitalocean": {
        "image_format": "qcow2",
        "image_suffix": "qcow2.gz",
        "platform": "digitalocean",
        "gzip": True
    },
    "gcp": {
        # See https://cloud.google.com/compute/docs/import/import-existing-image#requirements_for_the_image_file
        "image_format": "raw",
        "platform": "gcp",
        "image_suffix": "tar.gz",
        "gzip": True,
        "convert_options": {
            '-o': 'preallocation=off'
        },
        "tar_members": [
            "disk.raw"
        ],
        "tar_flags": [
            DEFAULT_TAR_FLAGS,
            "--format=oldgnu"
        ]
    },
    "openstack": {
        "image_format": "qcow2",
        "platform": "openstack",
    },
    "nutanix": {
        "image_format": "qcow2",
        "platform": "nutanix",
    },
    "vmware_vmdk": {
        "image_format": "vmdk",
        "image_suffix": "vmdk",
        "platform": "vmware",
        "convert_options":  {
            '-o': 'adapter_type=lsilogic,subformat=streamOptimized,compat6'
        }
    },
    "vultr": {
        "image_format": "raw",
        "platform": "vultr",
    },
    "exoscale": {
        "image_format": "qcow2",
        "platform": "exoscale",
    }
}


class ImageError(Exception):
    """
    Base error for build issues.
    """
    pass


def get_qemu_variant(variant, parser, kwargs={}):
    """
    Helper function get get the QemuVariantImage Build Obj
    """
    log.debug(f"returning QemuVariantImage for {variant}")
    return QemuVariantImage(
        buildroot=parser.buildroot,
        build=parser.build,
        schema=parser.schema,
        variant=variant,
        force=parser.force,
        arch=parser.arch,
        compress=parser.compress,
        **kwargs)


class QemuVariantImage(_Build):
    def __init__(self, **kwargs):
        """
        This takes all the regular _BuildClass arguments. In kwargs, the
        additional arguments are used:
            image_format: standard qemu types
            convert_options: optional qemu arguments
            platform: the name of the image.
            platform_image_name: in case you want to use a different name
            virtual_size: in case you want to explicitly set a virtual size

        Alternatively, you can provide "variant=<variant>" and defaults will be
        used.
        """
        self._image_name = None
        variant = kwargs.pop("variant", False)
        if variant:
            kwargs.update(VARIANTS.get(variant, {}))
        self.image_format = kwargs.pop("image_format", "raw")
        self.image_suffix = kwargs.pop("image_suffix", self.image_format)
        self.convert_options = kwargs.pop("convert_options", {})
        self.mutate_callback = kwargs.pop("mutate-callback", None)
        self.platform = kwargs.pop("platform", "qemu")
        self.force = kwargs.get("force", False)
        self.compress = kwargs.get("compress", False)
        self.tar_members = kwargs.pop("tar_members", None)
        self.tar_flags = kwargs.pop("tar_flags", [DEFAULT_TAR_FLAGS])
        self.gzip = kwargs.pop("gzip", False)
        self.virtual_size = kwargs.pop("virtual_size", None)

        # this is used in case the image has a different disk
        # name than the platform
        self.platform_image_name = kwargs.get(
            "platform_image_name", self.platform)

        _Build.__init__(self, **kwargs)

    @property
    def image_qemu(self):
        """
        Return the path of the Qemu QCOW2 image from the meta-data
        """
        qemu_meta = self.meta.get_artifact_meta("qemu", unmerged=True)
        qimage = os.path.join(
            self.build_dir,
            qemu_meta.get('images', {}).get('qemu', {}).get('path', None)
        )
        if not qimage:
            raise ImageError("qemu image has not be built yet")
        elif not os.path.exists(qimage):
            raise ImageError(f"{qimage} does not exist")
        return qimage

    @property
    def image_name(self):
        return f'{self.image_name_base}.{self.image_suffix}'

    @property
    def tmp_image(self):
        tmp_image_base = os.path.basename(self.image_qemu)
        return os.path.join(self._tmpdir, f"{tmp_image_base}.working")

    @property
    def image_meta(self):
        try:
            return self.meta["images"][self.platform]
        except Exception:
            return None

    def set_platform(self):
        run_verbose(['/usr/lib/coreos-assembler/gf-platformid',
                     self.image_qemu, self.tmp_image, self.platform])

    def mutate_image(self):
        """
        mutate_image is broken out seperately to allow other Classes to
        override the behavor.

        The callback parameter used to do post-processing on the working
        image before commiting it to the final location. To see how
        this is done, look at cosalib.vmware.VMwareOVA.mutate_image.

        :param callback: callback function for extra processing image
        :type callback: function
        """
        work_img = os.path.join(self._tmpdir,
                                f"{self.image_name_base}.{self.image_format}")
        final_img = os.path.join(os.path.abspath(self.build_dir),
                                 self.image_name)
        meta_patch = {}

        log.info(f"Staging temp image: {work_img}")
        self.set_platform()

        # Resizing if requested
        if self.virtual_size is not None:
            resize_cmd = ['qemu-img', 'resize',
                          self.tmp_image, self.virtual_size]
            run_verbose(resize_cmd)

        cmd = ['qemu-img', 'convert', '-f', 'qcow2', '-O',
               self.image_format, self.tmp_image]
        for k, v in self.convert_options.items():
            cmd.extend([k, v])
        cmd.extend([work_img])
        run_verbose(cmd)

        img_info = image_info(work_img)
        if self.image_format != img_info.get("format"):
            raise ImageError((f"{work_img} format mismatch"
                              f" expected: '{self.image_format}'"
                              f" found: '{img_info.get('format')}'"))

        if self.mutate_callback:
            log.info("Processing work image callback")
            meta_patch.update(self.mutate_callback(work_img) or {})

        if self.tar_members:
            # Python does not create sparse Tarfiles, so we have do it via
            # the CLI here.
            tarlist = []
            for member in self.tar_members:
                member_name = os.path.basename(member)
                # In the case of several clouds, the disk is named
                # `disk.raw` or `disk.vmdk`.  When creating a tarball, we
                # rename the disk to the in-tar name if the name does not
                # match the default naming.
                if member_name.endswith(('.raw', '.vmdk')):
                    if member_name != os.path.basename(work_img):
                        shutil.move(work_img, os.path.join(self._tmpdir, member_name))
                tarlist.append(member_name)

            tar_cmd = ['tar', '--owner=0', '--group=0', '-C', self._tmpdir]
            tar_cmd.extend(self.tar_flags)
            tar_cmd.extend(['-f', final_img])
            tar_cmd.extend(tarlist)
            run_verbose(tar_cmd)

        else:
            log.info(f"Moving {work_img} to {final_img}")
            shutil.move(work_img, final_img)

        if self.gzip:
            sha256 = sha256sum_file(final_img)
            size = os.stat(final_img).st_size
            temp_path = f"{final_img}.tmp"
            with open(temp_path, "wb") as fh:
                run_verbose(['gzip', '-9c', final_img], stdout=fh)
            shutil.move(temp_path, final_img)
            meta_patch.update({
                'skip-compression': True,
                'uncompressed-sha256': sha256,
                'uncompressed-size': size,
            })

        return meta_patch

    def _build_artifacts(self, *args, **kwargs):
        """
        Implements Super()._build_artifacts

        :param args: Non keyword arguments to pass to add_argument
        :type args: list
        :param kwargs: Keyword arguments to pass to add_argument
        :type kwargs: dict
        """
        if self.have_artifact and not self.force:
            raise BuildExistsError(
                f"{self.image_name} has already been built")

        meta_patch = self.mutate_image()
        imgs = self.meta.get("images", {})
        img_meta = self.get_artifact_meta()
        img_meta.update(meta_patch or {})
        self._found_files[self.image_name] = img_meta
        imgs[self.platform] = img_meta
        self.meta_write(artifact_name=self.platform_image_name)
        if self.compress:
            subprocess.check_call(['cosa', 'compress', '--artifact=' + self.platform])
