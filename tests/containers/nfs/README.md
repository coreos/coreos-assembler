# NFS Server Container

This is used by the `kdump.crash.nfs` test.

This image is forked from the [openshift e2e test image](https://github.com/openshift/kubernetes/tree/7ca9eb1e9e5ced974033c2b6f26560e22535244c/test/images/volume/nfs)

See https://github.com/coreos/coreos-assembler/pull/3911 for the inital PR using it for more details on the test.

It serves an empty `/` directory, writeable by anyone.
Not for production use!
