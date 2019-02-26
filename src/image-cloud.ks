# ip=dhcp and rd.neednet=1 enable networking in the initramfs
# We use net.ifnames in cloud environments
# no_timer_check is something we're cargo culting around
# console= args are also for clouds
# The other ones are for Ignition and are also in image-metal.ks;
# change them there first.
bootloader --timeout=1 --append="no_timer_check console=ttyS0,115200n8 console=tty0 net.ifnames=0 biosdevname=0 ip=dhcp rd.neednet=1 rootflags=defaults,prjquota rw $ignition_firstboot"

%post --erroronfail
# By default, we do DHCP.  Also, due to the above disabling
# of biosdevname/net.ifnames, this uses eth0.
# The DHCP_CLIENT_ID="mac" bit is so that we match what dhclient
# does in the initrd, and the DHCP server gives us the same lease
# if possible. See https://github.com/coreos/fedora-coreos-config/issues/58.
cat <<EOF > /etc/sysconfig/network-scripts/ifcfg-eth0
DEVICE="eth0"
BOOTPROTO="dhcp"
ONBOOT="yes"
TYPE="Ethernet"
PERSISTENT_DHCLIENT="yes"
NM_CONTROLLED="yes"
DHCP_CLIENT_ID="mac"
EOF
%end
