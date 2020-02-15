#!/usr/bin/env python3
# NOTE: PYTHONUNBUFFERED is set in cmdlib.sh for unbuffered output
#
# An operation that mutates a build by generating an ova
import logging as log
import os.path
import sys

cosa_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, f"{cosa_dir}/cosalib")
sys.path.insert(0, cosa_dir)

from cosalib.cmdlib import image_info
from cosalib.qemuvariants import QemuVariantImage


OVA_TEMPLATE_FILE = '/usr/lib/coreos-assembler/vmware-template.xml'


# Variant are OVA types that are derived from qemu images.
# To define new variants that use the QCOW2 disk image, simply,
# add its definition below:
VARIANTS = {
    "vmware": {
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


class VmwareOVA(QemuVariantImage):
    """
    VmwareOVA's are based on the QemuVariant Image. This Class tries to do
    the absolute bare minium, and is effectively a wrapper around the
    QemuVariantImage Class. The only added functionality is the generation
    of the OVF paramters.

    Spec for an OVA can be found at:
    https://www.dmtf.org/sites/default/files/standards/documents/DSP0243_1.1.0.pdf
    """

    def __init__(self, *args, **kwargs):
        variant = kwargs.pop("variant", "vmware")
        kwargs.update(VARIANTS.get(variant, {}))
        QemuVariantImage.__init__(self, *args, **kwargs)
        # Set the QemuVariant mutate_callback so that OVA is called.
        self.mutate_callback = self.write_ova
        # Ensure that desc.ovf is included in the tar
        self.desc_ovf_path = os.path.join(self._tmpdir, "desc.ovf")

    def generate_ovf_parameters(self, vmdk, cpu=2,
                                memory=4096, system_type="vmx-13",
                                os_type="rhel7_64Guest", scsi="VirtualSCSI",
                                network="VmxNet3"):
        """
        Returns a dictionary with the parameters needed to create an OVF file
        based on the qemu, vmdk, and info from the build metadata
        """
        disk_info = image_info(vmdk)
        image = self.summary
        product = f'{self.meta["name"]} {self.summary}'
        vendor = self.meta['name']
        version = self.meta['ostree-version']

        params = {
            'ovf_cpu_count':                    cpu,
            'ovf_memory_mb':                    memory,
            'vsphere_image_name':               image,
            'vsphere_product_name':             product,
            'vsphere_product_vendor_name':      vendor,
            'vsphere_product_version':          version,
            'vsphere_virtual_system_type':      system_type,
            'vsphere_os_type':                  os_type,
            'vsphere_scsi_controller_type':     scsi,
            'vsphere_network_controller_type':  network,
            'virtual_disk_size':                disk_info.get("virtual-size"),
            'vmdk_size':                        disk_info.get("actual-size"),
        }

        return params

    def write_ova(self, image_name):
        """
        write_ova file against a vmdk disk.

        :param image_name: name of image to create OVF parameters for.
        :type image_name: str
        """
        ovf_params = self.generate_ovf_parameters(image_name)

        with open(OVA_TEMPLATE_FILE) as f:
            template = f.read()
        vmdk_xml = template.format(**ovf_params)

        with open(self.desc_ovf_path, "w") as ovf:
            ovf.write(vmdk_xml)

        log.debug(vmdk_xml)
        log.info("desc.ovf will be added to the tar file")
        self.tar_members.append(self.desc_ovf_path)
