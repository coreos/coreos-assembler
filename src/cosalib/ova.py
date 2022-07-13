#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in cmdlib.sh for unbuffered output
#
# An operation that mutates a build by generating an ova
import logging as log
import os.path
import sys
import json

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.cmdlib import image_info
from cosalib.qemuvariants import QemuVariantImage


OVA_TEMPLATE_DIR = '/usr/lib/coreos-assembler'


# Variant are OVA types that are derived from qemu images.
# To define new variants that use the QCOW2 disk image, simply,
# add its definition below:
VARIANTS = {
    "virtualbox": {
        'template': 'virtualbox-template.xml',
        "image_format": "vmdk",
        "image_suffix": "ova",
        "platform": "virtualbox",
        "convert_options":  {
            '-o': 'subformat=streamOptimized'
        },
        "tar_members": [
            "disk.vmdk"
        ],
        "tar_flags": [
            # DEFAULT_TAR_FLAGS has -S, which isn't suppported by ustar
            '-ch',
            # Required by OVF spec
            "--format=ustar"
        ]
    },
    "vmware": {
        'template': 'vmware-template.xml',
        "image_format": "vmdk",
        "image_suffix": "ova",
        "platform": "vmware",
        "convert_options":  {
            '-o': 'adapter_type=lsilogic,subformat=streamOptimized,compat6'
        },
        "tar_members": [
            "disk.vmdk"
        ],
        "tar_flags": [
            # DEFAULT_TAR_FLAGS has -S, which isn't suppported by ustar
            '-ch',
            # Required by OVF spec
            "--format=ustar"
        ]
    },
}


class OVA(QemuVariantImage):
    """
    OVA's are based on the QemuVariant Image. This Class tries to do
    the absolute bare minium, and is effectively a wrapper around the
    QemuVariantImage Class. The only added functionality is the generation
    of the OVF paramters.

    Spec for an OVA can be found at:
    https://www.dmtf.org/sites/default/files/standards/documents/DSP0243_1.1.0.pdf
    """

    def __init__(self, **kwargs):
        variant = kwargs.pop("variant", "vmware")
        kwargs.update(VARIANTS.get(variant, {}))
        self.template_name = kwargs.pop('template')
        QemuVariantImage.__init__(self, **kwargs)
        # Set the QemuVariant mutate_callback so that OVA is called.
        self.mutate_callback = self.write_ova
        # Ensure that coreos.ovf is included in the tar
        self.ovf_path = os.path.join(self._tmpdir, "coreos.ovf")

    def generate_ovf_parameters(self, vmdk, cpu=2, memory=4096):
        """
        Returns a dictionary with the parameters needed to create an OVF file
        based on the qemu, vmdk, image.yaml, and info from the build metadata
        """
        with open(os.path.join(self._workdir, 'tmp/image.json')) as f:
            image_json = json.load(f)

        secure_boot = 'true' if image_json['vmware-secure-boot'] else 'false'
        system_type = 'vmx-{}'.format(image_json['vmware-hw-version'])
        os_type = image_json['vmware-os-type']
        disk_info = image_info(vmdk)
        vmdk_size = os.stat(vmdk).st_size
        image = self.summary
        product = f'{self.meta["name"]} {self.summary}'
        vendor = self.meta['name']
        version = self.meta['ostree-version']

        params = {
            'ovf_cpu_count':                    cpu,
            'ovf_memory_mb':                    memory,
            'secure_boot':                      secure_boot,
            'vsphere_image_name':               image,
            'vsphere_product_name':             product,
            'vsphere_product_vendor_name':      vendor,
            'vsphere_product_version':          version,
            'vsphere_virtual_system_type':      system_type,
            'vsphere_os_type':                  os_type,
            'vmdk_capacity':                    disk_info.get("virtual-size"),
            'vmdk_size':                        str(vmdk_size),
        }

        return params

    def write_ova(self, image_name):
        """
        write_ova file against a vmdk disk.

        :param image_name: name of image to create OVF parameters for.
        :type image_name: str
        """
        ovf_params = self.generate_ovf_parameters(image_name)

        with open(os.path.join(OVA_TEMPLATE_DIR, self.template_name)) as f:
            template = f.read()
        ovf_xml = template.format(**ovf_params)

        with open(self.ovf_path, "w") as ovf:
            ovf.write(ovf_xml)

        log.debug(ovf_xml)
        # OVF descriptor must come first, then the manifest, then
        # References in order
        self.tar_members.insert(0, self.ovf_path)

        return {
            'skip-compression': True,
        }
