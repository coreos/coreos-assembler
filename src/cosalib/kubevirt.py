import os
import logging as log

from cosalib.cmdlib import runcmd
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

    def write_oci(self, _):
        """
        Take the qcow2 base image and convert it to an oci-archive.
        """
        ctxdir = self._tmpdir
        final_img = os.path.join(os.path.abspath(self.build_dir), self.image_name)
        # Create the Containerfile to use for the build
        with open(os.path.join(ctxdir, "Containerfile"), "w") as f:
            f.write(f"FROM scratch\nADD {self.image_name_base}.{self.image_format} /disk/\n")
        # Run the build inside a supermin VM because in OpenShift things are extremely
        # locked down. Here we are doing two things that are interesting. We are using
        # a virtio-serial device to write the output file to because we've seen 9p
        # issues in our pipeline. We're also using an undocumented feature of podman-build,
        # which is to write directly to an oci-archive via --tag=oci-archive:file.ociarchive
        # https://github.com/containers/buildah/issues/4740.
        runcmd(['/usr/lib/coreos-assembler/runvm.sh',
                '-chardev', f'file,id=ociarchiveout,path={final_img}',
                '-device', 'virtserialport,chardev=ociarchiveout,name=ociarchiveout',
                '--', 'podman', 'build', '--disable-compression=false',
                '--label', f'version={self.build_id}',
                '--tag=oci-archive:/dev/virtio-ports/ociarchiveout', ctxdir])


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
