#!/usr/bin/env python3

import struct
import tempfile
import os
import sys
import json
from subprocess import check_call
from stat import *
from shutil import rmtree
import tarfile

# Usage: generate-ova-from-template.py input_image.qcow2 input_template.xml example_ova_parameters.json output_image.ova

def qcow_disk_size(filename):
    """
    Detect if an image is in qcow format
    If it is, return the size
    If it isn't, return None
    Borrowed from Image Factory and modified

    For interested parties, this is the QCOW header struct in C
    struct qcow_header {
       uint32_t magic;
       uint32_t version;
       uint64_t backing_file_offset;
       uint32_t backing_file_size;
       uint32_t cluster_bits;
       uint64_t size; /* in bytes */
       uint32_t crypt_method;
       uint32_t l1_size;
       uint64_t l1_table_offset;
       uint64_t refcount_table_offset;
       uint32_t refcount_table_clusters;
       uint32_t nb_snapshots;
       uint64_t snapshots_offset;
    };
    """

    # And in Python struct format string-ese
    qcow_struct=">IIQIIQIIQQIIQ" # > means big-endian
    qcow_magic = 0x514649FB # 'Q' 'F' 'I' 0xFB

    f = open(filename,"rb")
    pack = f.read(struct.calcsize(qcow_struct))
    f.close()

    unpack = struct.unpack(qcow_struct, pack)

    if unpack[0] == qcow_magic:
        # uint64_t size  <--- from above
        return unpack[5]
    else:
        return None

input_image = sys.argv[1]
input_template_file = sys.argv[2]
input_template = open(input_template_file).read()
image_parameters_file = sys.argv[3]
image_parameters = json.loads(open(image_parameters_file).read())
output_image = sys.argv[4]

vmdk_working_path = tempfile.mkdtemp(dir="/tmp")
os.chmod(vmdk_working_path, S_IRUSR|S_IWUSR|S_IXUSR|S_IRGRP|S_IXGRP|S_IROTH|S_IXOTH)

vmdk_image = os.path.join(vmdk_working_path, "disk.vmdk")
check_call([ "qemu-img", "convert", "-f", "qcow2", "-O", "vmdk", "-o", 
             "adapter_type=lsilogic,subformat=streamOptimized,compat6",
             input_image, vmdk_image ])

virtual_disk_size = str(qcow_disk_size(input_image))
vmdk_size = str(os.stat(vmdk_image).st_blocks*512)

image_parameters['virtual_disk_size'] = virtual_disk_size
image_parameters['vmdk_size'] = vmdk_size
vmdk_xml = input_template.format(**image_parameters)

vmdk_xml_filename = os.path.join(vmdk_working_path, "desc.ovf")
with open(vmdk_xml_filename, "w") as vmdk_xml_file:
    vmdk_xml_file.write(vmdk_xml)

with tarfile.open(output_image, 'w') as tar:
    tar.add(vmdk_xml_filename, arcname="desc.ovf")
    tar.add(vmdk_image, arcname="disk.vmdk")

rmtree(vmdk_working_path)
