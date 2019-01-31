# If you add something here, also update image-cloud.ks.
# prjquota is for quota enablement for containers: https://bugzilla.redhat.com/show_bug.cgi?id=1658386
# rw and $ignition_firstboot are used by https://github.com/coreos/ignition-dracut/
bootloader --append="rootflags=defaults,prjquota rw $ignition_firstboot"
