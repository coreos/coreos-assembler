#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in cmdlib.sh for unbuffered output
#
# An operation that mutates a build by generating an ova
import subprocess
import logging as log
import urllib
import os.path
import sys
from cosalib.cmdlib import (
    runcmd
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
        "image_suffix": "ova.gz",
        "platform": "powervs",
        "compression": "gzip",
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

    def generate_ovf_parameters(self, raw):
        """
        Returns a dictionary with the parameters needed to create an OVF and meta file
        based on the qemu, raw, and info from the build metadata
        """
        image_size = os.stat(raw).st_size
        image = f'{self.meta["name"]}-{self.meta["ostree-version"]}'
        image_description = f'{self.meta["name"]} {self.meta["summary"]} {self.meta["ostree-version"]}'

        params = {
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

        with open(self.ovf_path, "w") as ovf:
            ovf.write(ovf_xml)

        log.debug(ovf_xml)
        # OVF descriptor must come first, then the manifest, then the meta file
        self.tar_members.append(self.ovf_path)


@retry(reraise=True, stop=stop_after_attempt(3))
def ibmcloud_run_ore(build, args):
    ore_args = ['ore']
    if args.log_level:
        ore_args.extend(['--log-level', args.log_level])

    region = "us-east"
    if args.region is not None and len(args.region) > 0:
        region = args.region[0]

    platform = args.target
    if args.cloud_object_storage is not None:
        cloud_object_storage = args.cloud_object_storage
    else:
        cloud_object_storage = f"coreos-dev-image-{platform}"

    ibmcloud_object_name = f"{build.build_name}-{build.build_id}-{build.basearch}-{build.platform}"
    if platform == "powervs":
        # powervs requires the image name to have an extension and also does not
        # tolerate dots in the name. It affects the internal import from IBMCloud
        # to the PowerVS systems
        ibmcloud_object_name = ibmcloud_object_name.replace(".", "-") + ".ova.gz"

    ore_args.extend([
        'ibmcloud', 'upload',
        '--region', f"{region}",
        '--cloud-object-storage', f"{cloud_object_storage}",
        '--bucket', f"{args.bucket}",
        '--name', ibmcloud_object_name,
        '--file', f"{build.image_path}",
    ])

    if args.credentials_file is not None:
        ore_args.extend(['--credentials-file', f"{args.credentials_file}"])

    if args.force:
        ore_args.extend(['--force'])

    runcmd(ore_args)
    url_path = urllib.parse.quote((
        f"s3.{region}.cloud-object-storage.appdomain.cloud/"
        f"{args.bucket}/{ibmcloud_object_name}"
    ))

    build.meta[platform] = [{
        'object': ibmcloud_object_name,
        'bucket': args.bucket,
        'region': region,
        'url': f"https://{url_path}",
    }]
    build.meta_write()  # update build metadata


def ibmcloud_run_ore_replicate(build, args):
    build.refresh_meta()
    platform = args.target
    if platform == "powervs":
        ibmcloud_img_data = build.meta.get('powervs', [])
    else:
        ibmcloud_img_data = build.meta.get('ibmcloud', [])
    if len(ibmcloud_img_data) < 1:
        raise SystemExit(("buildmeta doesn't contain source images. "
                        "Run buildextend-{platform} first"))

    # define regions - https://cloud.ibm.com/docs/power-iaas?topic=power-iaas-creating-power-virtual-server#creating-service
    # PowerVS insatnces are supported in all the regions where cloud object storage can be created. This list is common for
    # both IBMCloud and PowerVS.
    if not args.region:
        args.region = ['au-syd', 'br-sao', 'ca-tor', 'eu-de', 'eu-es', 'eu-gb', 'jp-osa', 'jp-tok', 'us-east', 'us-south']
        log.info(("default: replicating to all regions. If this is not "
                 " desirable, use '--regions'"))

    log.info("replicating to regions: %s", args.region)

    # only replicate to regions that don't already exist
    existing_regions = [item['region'] for item in ibmcloud_img_data]
    duplicates = list(set(args.region).intersection(existing_regions))
    if len(duplicates) > 0:
        print((f"Images already exist in {duplicates} region(s)"
               ", skipping listed region(s)..."))
    region_list = list(set(args.region) - set(duplicates))
    if len(region_list) == 0:
        print("no new regions detected")
        sys.exit(0)

    source_object = ibmcloud_img_data[0]['object']
    source_bucket = ibmcloud_img_data[0]['bucket']

    if args.cloud_object_storage is not None:
        cloud_object_storage = args.cloud_object_storage
    else:
        cloud_object_storage = f"coreos-dev-image-{platform}"

    if args.bucket_prefix is not None:
        bucket_prefix = args.bucket_prefix
    else:
        bucket_prefix = f"coreos-dev-image-{platform}"

    ore_args = [
        'ore',
        '--log-level', args.log_level,
        'ibmcloud', 'copy-object',
        '--cloud-object-storage', cloud_object_storage,
        '--source-name', source_object,
        '--source-bucket', source_bucket
    ]

    if args.credentials_file is not None:
        ore_args.extend(['--credentials-file', f"{args.credentials_file}"])

    upload_failed_in_region = None

    for upload_region in region_list:
        region_ore_args = ore_args.copy() + ['--destination-region', upload_region,
                                            '--destination-bucket', f"{bucket_prefix}-{upload_region}"]
        print("+ {}".format(subprocess.list2cmdline(region_ore_args)))
        try:
            subprocess.check_output(region_ore_args)
        except subprocess.CalledProcessError:
            upload_failed_in_region = upload_region
            break

        url_path = urllib.parse.quote((
            f"s3.{upload_region}.cloud-object-storage.appdomain.cloud/"
            f"{bucket_prefix}-{upload_region}/{source_object}"
        ))

        ibmcloud_img_data.extend([
            {
                'object': source_object,
                'bucket': f"{bucket_prefix}-{upload_region}",
                'region': upload_region,
                'url': f"https://{url_path}"
            }
        ])

    build.meta[platform] = ibmcloud_img_data
    build.meta_write()

    if upload_failed_in_region is not None:
        raise Exception(f"Upload failed in {upload_failed_in_region} region")
    pass


def ibmcloud_cli(parser):
    parser.add_argument("--bucket", help="S3 Bucket")
    parser.add_argument("--bucket-prefix", help="S3 Bucket prefix to replicate across regional buckets")
    parser.add_argument("--cloud-object-storage", help="IBMCloud cloud object storage to upload to")
    parser.add_argument("--credentials-file", help="Path to IBMCloud auth file")
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
        arch=parser.arch,
        compress=parser.compress,
        **kwargs)
