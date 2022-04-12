import os
import subprocess
import logging as log

from cosalib.cmdlib import (
    run_verbose,
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
        buildah_img = run_verbose_collect_stdout(buildah_base_argv + ["from", "scratch"])
        run_verbose(buildah_base_argv + ["add", "--chmod", "0555", buildah_img, image_name, "/disk/coreos.img"])
        digest = run_verbose_collect_stdout(buildah_base_argv + ["commit", buildah_img])
        run_verbose(buildah_base_argv + ["push", "--format", "oci", digest, f"oci-archive:{final_img}"])


def kubevirt_run_ore(build, args):
    if not args.repository:
        raise Exception("--repository must not be empty")

    name = f"{build.build_name}"
    if args.name is not None:
        name = args.name
    tag = f"{build.build_id}-{build.basearch}"
    full_name = os.path.join(args.repository, name)

    digest = run_verbose(["skopeo", "inspect", f"oci-archive:{build.image_path}", "-f", "{{.Digest}}"],
                         stdout=subprocess.PIPE).stdout.decode("utf-8").strip()
    log.info(f"pushing {full_name}:{tag} with digest {digest}")
    run_verbose(["skopeo", "copy", f"oci-archive:{build.image_path}", f"docker://{full_name}:{tag}"])

    build.meta['kubevirt'] = {
        'image': f"{full_name}@{digest}",
    }
    build.meta_write()


def kubevirt_run_ore_replicate(*args, **kwargs):
    print("""
KubeVirt does not require regional replication. This command is a
placeholder.
""")


def kubevirt_cli(parser):
    parser.add_argument("--name",
                        help="Name to append to the repository (e.g. fedora-coreos). Defaults to the build name.")
    parser.add_argument("--repository", help="repository to push to (e.g. quay.io or quay.io/myorg)")
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


def run_verbose_collect_stdout(args):
    return run_verbose(args, stdout=subprocess.PIPE).stdout.decode("utf-8").strip()
