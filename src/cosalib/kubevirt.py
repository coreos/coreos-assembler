import os
import logging as log

from cosalib.cmdlib import (
    runcmd,
)

from cosalib.buildah import (
    buildah_base_args
)

from cosalib.qemuvariants import QemuVariantImage


class KubeVirtImage(QemuVariantImage):
    """
    KubeVirtImage uses QemuVariantImage to create a normal qcow2 image.
    This image is then wrapped into an ociarchive as final build artifact which
    can be pushed to container registries and used as ContainerDisk in kubevirt.
    """

    def __init__(self, **kwargs):
        QemuVariantImage.__init__(self, **kwargs)
        # Set the QemuVariant mutate_callback so that OVA is called.
        self.mutate_callback = self.write_oci
        self.mutate_callback_creates_final_image = True

    def write_oci(self, image_name):
        """
        Take the qcow2 base image and convert it to an oci-archive.
        """
        buildah_base_argv = buildah_base_args()
        final_img = os.path.join(os.path.abspath(self.build_dir),
                                 self.image_name)
        cmd = buildah_base_argv + ["from", "scratch"]
        buildah_img = runcmd(cmd, capture_output=True).stdout.decode("utf-8").strip()
        runcmd(buildah_base_argv + ["add", "--chmod", "0555", buildah_img, image_name, "/disk/coreos.img"])
        cmd = buildah_base_argv + ["commit", buildah_img]
        digest = runcmd(cmd, capture_output=True).stdout.decode("utf-8").strip()
        runcmd(buildah_base_argv + ["push", "--format", "oci", digest, f"oci-archive:{final_img}"])


def kubevirt_run_ore(build, args):
    """
    This function is not necessary for Kubevirt. We'll push the ociarchive
    files using cosa push-container-manifest in the release job.
    """
    raise Exception("not implemented")


def kubevirt_run_ore_replicate(*args, **kwargs):
    """
    This function is not necessary for Kubevirt. We'll push the ociarchive
    files using cosa push-container-manifest in the release job.
    """
    raise Exception("not implemented")


def kubevirt_cli(parser):
    return parser


def get_kubevirt_variant(variant, parser, kwargs={}):
    """
    Helper function to get the KubeVirtCloudImage Build Obj
    """
    log.debug(f"returning KubeVirtCloudImage for {variant}")
    return KubeVirtImage(
        buildroot=parser.buildroot,
        build=parser.build,
        schema=parser.schema,
        variant=variant,
        force=parser.force,
        arch=parser.arch,
        compress=parser.compress,
        **kwargs)
