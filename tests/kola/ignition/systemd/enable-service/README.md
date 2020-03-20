enable nfs-server, touch /etc/exports as it doesn't exist by default on Container Linux,
and touch /var/lib/nfs/etab (https://bugzilla.redhat.com/show_bug.cgi?id=1394395) for RHCOS

FCOS just ships the client (see
https://github.com/coreos/fedora-coreos-tracker/issues/121).
Should probably just pick a different unit to test with, though
testing the NFS workflow is useful for RHCOS.