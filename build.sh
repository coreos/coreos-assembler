#!/usr/bin/env bash
set -euo pipefail

# Detect what platform we are on
if ! grep -q '^Fedora' /etc/redhat-release; then
    echo 1>&2 "should be on either Fedora"
    exit 1
fi

arch=$(uname -m)

if [ $# -gt 1 ]; then
  echo Usage: "build.sh [CMD]"
  echo "Supported commands:"
  echo "    configure_user"
  echo "    configure_yum_repos"
  echo "    install_rpms"
  echo "    make_and_makeinstall"
  echo "    install_tang"
  exit 1
fi

set -x
srcdir=$(pwd)

configure_yum_repos() {
    local version_id
    version_id=$(. /etc/os-release && echo ${VERSION_ID})
    # Add continuous tag for latest build tools and mark as required so we
    # can depend on those latest tools being available in all container
    # builds.
    echo -e "[f${version_id}-coreos-continuous]\nenabled=1\nmetadata_expire=1m\nbaseurl=https://kojipkgs.fedoraproject.org/repos-dist/f${version_id}-coreos-continuous/latest/\$basearch/\ngpgcheck=0\nskip_if_unavailable=False\n" > /etc/yum.repos.d/coreos.repo
    # We have a few packages that we currently import from f32, so explicitly
    # add the f32 repo temporarily.
    echo -e "[f32-coreos-continuous]\nenabled=1\nmetadata_expire=1m\nbaseurl=https://kojipkgs.fedoraproject.org/repos-dist/f32-coreos-continuous/latest/\$basearch/\ngpgcheck=0\nskip_if_unavailable=False\n" >> /etc/yum.repos.d/coreos.repo

}

install_rpms() {
    # First, a general update; this is best practice.  We also hit an issue recently
    # where qemu implicitly depended on an updated libusbx but didn't have a versioned
    # requires https://bugzilla.redhat.com/show_bug.cgi?id=1625641
    yum -y distro-sync

    # xargs is part of findutils, which may not be installed
    yum -y install /usr/bin/xargs

    # These are only used to build things in here.  Today
    # we ship these in the container too to make it easier
    # to use the container as a development environment for itself.
    # Down the line we may strip these out, or have a separate
    # development version.
    builddeps=$(grep -v '^#' "${srcdir}"/src/build-deps.txt)

    # Process our base dependencies + build dependencies and install
    (echo "${builddeps}" && "${srcdir}"/src/print-dependencies.sh) | xargs yum -y install

    # Commented out for now, see above
    #dnf remove -y ${builddeps}
    # can't remove grubby on el7 because libguestfs-tools depends on it
    rpm -q grubby && yum remove -y grubby

    # Allow Kerberos Auth to work from a keytab. The keyring is not
    # available in a Container.
    sed -e "s/^.*default_ccache_name/#    default_ccache_name/g" -i /etc/krb5.conf

    # Open up permissions on /boot/efi files so we can copy them
    # for our ISO installer image, skip if not present
    if [ -e /boot/efi ]; then
        chmod -R a+rX /boot/efi
    fi
    # Similarly for kernel data and SELinux policy, which we want to inject into supermin
    chmod -R a+rX /usr/lib/modules /usr/share/selinux/targeted
    # Further cleanup
    yum clean all
}

# Yes, this is a hack that loses sane auditing around what git commit
# we used to build fcct, etc.  In the future we'll probably give in and package
# it or something, see also https://github.com/coreos/fedora-coreos-tracker/issues/235
build_fcct() {
    cd /tmp
    git clone https://github.com/coreos/fcct
    cd fcct
    git describe --tags --always > /usr/share/fcct-build.revision
    ./build
    fcct=$(find bin -type f -name fcct | head -1)
    install -m 0755 -D -t /usr/bin "${fcct}"
    cd /tmp
    rm fcct -rf
}

install_tang() {
    install -m 0755 -T "$srcdir"/src/tang/tangdw /usr/libexec/tangdw
}

make_and_makeinstall() {
    make && make install
}

configure_user(){
    # /dev/kvm might be bound in, but will have the gid from the host, and not all distros
    # a+rw permissions on /dev/kvm. create groups for all the common kvm gids and then add
    # builder to them.
    # systemd defaults to 0666 but other packages like qemu sometimes override this with 0660.
    # Adding the user to the kvm group should always work.

    # fedora uses gid 36 for kvm
    groupadd -g 78 -o -r kvm78   # arch, gentoo
    groupadd -g 124 -o -r kvm124 # debian
    groupadd -g 232 -o -r kvm232 # ubuntu

    # We want to run what builds we can as an unprivileged user;
    # running as non-root is much better for the libvirt stack in particular
    # for the cases where we have --privileged in the container run for other reasons.
    # At some point we may make this the default.
    useradd builder --uid 1000 -G wheel,kvm,kvm78,kvm124,kvm232
    echo '%wheel ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers.d/wheel-nopasswd
    # Contents of /etc/sudoers.d need not to be world writable
    chmod 600 /etc/sudoers.d/wheel-nopasswd
}

write_archive_info() {
    # shellcheck source=src/cmdlib.sh
    . "${srcdir}/src/cmdlib.sh"
    mkdir -p /cosa /lib/coreos-assembler
    touch -f /lib/coreos-assembler/.clean
    prepare_git_artifacts "${srcdir}" /cosa/coreos-assembler-git.tar.gz /cosa/coreos-assembler-git.json
}

if [ $# -ne 0 ]; then
  # Run the function specified by the calling script
  ${1}
else
  # Otherwise, just run all the steps
  configure_yum_repos
  install_rpms
  write_archive_info
  make_and_makeinstall
  configure_user
  install_tang
fi
