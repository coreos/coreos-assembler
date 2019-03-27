# no_timer_check is something we're cargo culting around
# console= args are also for clouds
# The other ones are for Ignition and are also in image-metal.ks;
# change them there first.
bootloader --timeout=1 --append="no_timer_check console=ttyS0,115200n8 console=tty0 rootflags=defaults,prjquota rw $ignition_firstboot"
