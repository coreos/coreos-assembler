#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in cmdlib.sh for unbuffered output
#
# An operation that mutates a build by generating an ova
import logging as log
import urllib
import os.path
import sys
from cosalib.cmdlib import (
    run_verbose,
    get_basearch
)
from tenacity import (
    retry,
    stop_after_attempt
)

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.qemuvariants import QemuVariantImage


OVA_TEMPLATE_FILE = '/usr/lib/coreos-assembler/powervs-template.xml'

template_meta = """os-type = {os}
architecture = {basearch}
vol1-file = {image}
vol1-type = boot"""

# Variant are OVA types that are derived from qemu images.
# To define new variants that use the QCOW2/raw disk image, simply,
# add its definition below:
VARIANTS = {
    "ibmcloud": {
        "image_format": "qcow2",
        "platform": "ibmcloud",
        "virtual_size": "100G",
    },
    "powervs": {
        "image_format": "raw",
        "image_suffix": "ova",
        "platform": "powervs",
        "tar_members": [
            "disk.raw"
        ]
    },
}


class IBMCloudImage(QemuVariantImage):
    """
    PowerVSOVA's are based on the QemuVariant Image. This Class tries to do
    the absolute bare minium, and is effectively a wrapper around the
    QemuVariantImage Class. The only added functionality is the generation
    of the OVF paramters.
    """

    def __init__(self, **kwargs):
        variant = kwargs.pop("variant", "ibmcloud")
        kwargs.update(VARIANTS.get(variant, {}))
        QemuVariantImage.__init__(self, **kwargs)
        # Set the QemuVariant mutate_callback so that OVA is called.
        if variant == "powervs":
            self.mutate_callback = self.write_ova
            # Ensure that coreos.ovf is included in the tar
            self.ovf_path = os.path.join(self._tmpdir, "coreos.ovf")
            # Ensure that coreos.meta is included in the tar
            self.meta_path = os.path.join(self._tmpdir, "coreos.meta")

    def generate_ovf_parameters(self, raw):
        """
        Returns a dictionary with the parameters needed to create an OVF and meta file
        based on the qemu, raw, and info from the build metadata
        """
        image_size = os.stat(raw).st_size
        image = f'{self.meta["name"]}-{self.meta["ostree-version"]}'
        image_description = f'{self.meta["name"]} {self.meta["summary"]} {self.meta["ostree-version"]}'

        params = {
            'os':                  os.path.basename(raw).split("-")[0],
            'basearch':            get_basearch(),
            'image_description':   image_description,
            'image':               image,
            'image_size':          str(image_size),
        }

        return params

    def write_ova(self, image_name):
        """
        write_ova file.

        :param image_name: name of image to create OVF parameters for.
        :type image_name: str
        """
        ovf_params = self.generate_ovf_parameters(image_name)

        with open(OVA_TEMPLATE_FILE) as f:
            template = f.read()
        ovf_xml = template.format(**ovf_params)

        meta_text = template_meta.format(**ovf_params)

        with open(self.ovf_path, "w") as ovf:
            ovf.write(ovf_xml)

        with open(self.meta_path, "w") as meta:
            meta.write(meta_text)

        log.debug(ovf_xml)
        # OVF descriptor must come first, then the manifest, then the meta file
        self.tar_members.append(self.ovf_path)
        self.tar_members.append(self.meta_path)


@retry(reraise=True, stop=stop_after_attempt(3))
def ibmcloud_run_ore(build, args):
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])

    if args.force:
        ore_args.extend(['--force'])

    region = "us-east"
    if args.region is not None and len(args.region) > 0:
        region = args.region[0]

    platform = args.target
    if args.cloud_object_storage is not None:
        cloud_object_storage = args.cloud_object_storage
    else:
        cloud_object_storage = f"coreos-dev-image-{platform}"

    # powervs requires the image name to have an extension and also does not tolerate dots in the name. It affects the internal import from IBMCloud to the PowerVS systems
    if platform == "powervs":
        build_id = build.build_id.replace(".", "-") + ".ova"
    else:
        build_id = build.build_id

    ibmcloud_object_name = f"{build.build_name}-{build_id}"
    ore_args.extend([
        'ibmcloud', 'upload',
        '--region', f"{region}",
        '--cloud-object-storage', f"{cloud_object_storage}",
        '--bucket', f"{args.bucket}",
        '--name', ibmcloud_object_name,
        '--file', f"{build.image_path}",
    ])

    run_verbose(ore_args)
    url_path = urllib.parse.quote((
        f"s3.{region}.cloud-object-storage.appdomain.cloud/"
        f"{args.bucket}/{ibmcloud_object_name}"
    ))

    build.meta[platform] = {
        'image': ibmcloud_object_name,
        'bucket': args.bucket,
        'region': region,
        'url': f"https://{url_path}",
    }
    build.meta_write()  # update build metadata


def ibmcloud_run_ore_replicate(build, args):
    pass


def ibmcloud_cli(parser):
    parser.add_argument("--bucket", help="S3 Bucket")
    parser.add_argument("--cloud-object-storage", help="IBMCloud cloud object storage to upload to")
    return parser


def get_ibmcloud_variant(variant, parser, kwargs={}):
    """
    Helper function to get the IBMCloudImage Build Obj
    """
    log.debug(f"returning IBMCloudImage for {variant}")
    return IBMCloudImage(
        buildroot=parser.buildroot,
        build=parser.build,
        schema=parser.schema,
        variant=variant,
        force=parser.force,
        **kwargs)
