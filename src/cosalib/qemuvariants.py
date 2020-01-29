"""
Provides a base abstration for building images.
"""

import logging as log
import os.path
import shutil
import sys
import tarfile

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.build import (
    _Build,
    BuildExistsError
)
from cosalib.cmdlib import (
    get_basearch,
    run_verbose,
    sha256sum_file
)


# BASEARCH is the current machine architecture
BASEARCH = get_basearch()

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
    },
    "openstack": {
        "image_format": "qcow2",
        "platform": "openstack",
    },
    "gcp": {
        "image_format": "raw",
        "platform": "gcp",
        "image_suffix": "tar.gz",
        "tar_members": [
            "disk.raw"
        ]
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
        parser.buildroot,
        parser.build,
        variant=variant,
        force=parser.force,
        **kwargs)


class QemuVariantImage(_Build):
    def __init__(self, *args, **kwargs):
        """
        This takes all the regular _BuildClass arguments. In kwargs, the
        additional arguments are used:
            image_format: standard qemu types
            convert_options: optional qemu arguments
            platform: the name of the image.
            platform_image_name: in case you want to use a different name

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
        self.platform = kwargs.pop("platform", "qemu")
        self.force = kwargs.get("force", False)
        self.tar_members = kwargs.pop("tar_members", None)

        # this is used in case the image has a different disk
        # name than the platform
        self.platform_image_name = kwargs.get(
            "platform_image_name", self.platform)

        _Build.__init__(self, *args, **kwargs)

    @property
    def image_qemu(self):
        """
        Return the path of the Qemu QCOW2 image from the meta-data
        """
        qimage = os.path.join(
            self.build_dir,
            self.meta.get('images', {}).get('qemu', {}).get('path', None)
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

    def mutate_image(self, callback=None):
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

        log.info(f"Staging temp image: {work_img}")
        self.set_platform()
        cmd = ['qemu-img', 'convert', '-f', 'qcow2', '-O',
               self.image_format, self.tmp_image]
        for k, v in self.convert_options.items():
            cmd.extend([k, v])
        cmd.extend([work_img])
        run_verbose(cmd)

        if callback:
            log.info(f"Processing work image callback")
            callback(work_img)

        if self.tar_members:
            wmode = 'w'
            if final_img.endswith('gz'):
                wmode = 'w:gz'
            elif final_img.endswith('xz'):
                wmode = 'w:xz'
            log.info(f"Preparing tarball with mode '{wmode}': {final_img}")
            with tarfile.open(final_img, wmode) as tar:
                base_disk = self.tar_members.pop(0)
                base_name = os.path.basename(base_disk)
                log.info(f" - adding base disk '{work_img}' as '{base_name}'")
                tar.add(work_img, arcname=base_name)

                for te in self.tar_members:
                    te_name = os.path.basename(te)
                    log.info(f" - adding additional file: {te_name}")
                    tar.add(te, arcname=te_name)

        else:
            log.info(f"Moving {work_img} to {final_img}")
            shutil.move(work_img, final_img)

    def get_artifact_meta(self, fname=None):
        """
        Get the articat's meta-data

        :param fname: name of file to get meta-data for
        :type fname: str
        """
        fsize = '{}'.format(os.stat(self.image_path).st_size)
        if fname is None:
            fname = self.image_name
        fpath = os.path.join(self.build_dir, fname)
        log.info(f"Calculating metadata for {fname}")
        return {
            "path": fname,
            "sha256": sha256sum_file(fpath),
            "size": int(fsize)
        }

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

        self.mutate_image()
        imgs = self.meta.get("images", {})
        img_meta = self.get_artifact_meta()
        self._found_files[self.image_name] = img_meta
        imgs[self.platform] = img_meta
        self.meta_write()
