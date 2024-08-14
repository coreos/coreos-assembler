#!/usr/bin/env bash
set -euo pipefail

# uncomment this if you want to control the version of `oc` that gets installed
#OCP_VERSION=4.12

# Keep this script idempotent for local development rebuild use cases:
# any consecutive runs should produce the same result.

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
  echo "    patch_osbuild"
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
}

install_rpms() {
    local builddeps
    local frozendeps

    frozendeps=""

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
    (echo "${builddeps}" && echo "${frozendeps}" && "${srcdir}"/src/print-dependencies.sh) | xargs yum -y install

    # Add fast-tracked packages here.  We don't want to wait on bodhi for rpm-ostree
    # as we want to enable fast iteration there.
    yum -y --enablerepo=updates-testing upgrade rpm-ostree ostree

    # Delete file that only exists on ppc64le because it is causing
    # sudo to not work.
    # https://bugzilla.redhat.com/show_bug.cgi?id=2082149
    rm -f /etc/security/limits.d/95-kvm-memlock.conf

    # Commented out for now, see above
    #dnf remove -y ${builddeps}
    # can't remove grubby on el7 because libguestfs-tools depends on it
    # Add --exclude for s390utils-base because we need it to not get removed.
    rpm -q grubby && yum remove --exclude=s390utils-base -y grubby

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

# For now, we ship `oc` in coreos-assembler as {Fedora,RHEL} CoreOS is an essential part of OCP4,
# and it is very useful to have in the same place/flow as where we do builds/tests related
# to CoreOS.
install_ocp_tools() {
    # If $OCP_VERSION is defined we'll grab that specific version.
    # Otherwise we'll get the latest.
    local url="https://mirror.openshift.com/pub/openshift-v4/${arch}/clients/ocp/latest${OCP_VERSION:+-$OCP_VERSION}/openshift-client-linux.tar.gz"
    curl -L "$url" | tar zxf - oc
    mv oc /usr/bin
}

# By default, we trust the official Red Hat GPG keys
trust_redhat_gpg_keys() {
    for f in /usr/share/distribution-gpg-keys/redhat/*; do
        local base
        base=$(basename "$f")
        if [ ! -e "/etc/pki/rpm-gpg/$base" ]; then
            # libdnf at least ignores symlinks, so we need to copy.
            # but might as well keep symlinks as symlinks.
            cp -vPt /etc/pki/rpm-gpg "$f"
        fi
    done
}

make_and_makeinstall() {
    make
    make install
    # Remove go build cache
    # https://github.com/coreos/coreos-assembler/issues/2872
    rm -rf /root/.cache/go-build
}

configure_user(){
    # /dev/kvm might be bound in, but will have the gid from the host, and not all distros
    # a+rw permissions on /dev/kvm. create groups for all the common kvm gids and then add
    # builder to them.
    # systemd defaults to 0666 but other packages like qemu sometimes override this with 0660.
    # Adding the user to the kvm group should always work.

    # fedora uses gid 36 for kvm
    getent group kvm78  || groupadd -g 78 -o -r kvm78   # arch, gentoo
    getent group kvm124 || groupadd -g 124 -o -r kvm124 # debian
    getent group kvm232 || groupadd -g 232 -o -r kvm232 # ubuntu

    # We want to run what builds we can as an unprivileged user;
    # running as non-root is much better for the libvirt stack in particular
    # for the cases where we have --privileged in the container run for other reasons.
    # At some point we may make this the default.
    getent passwd builder || useradd builder --uid 1000 -G wheel,kvm,kvm78,kvm124,kvm232
    echo '%wheel ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/wheel-nopasswd
    # Contents of /etc/sudoers.d need not to be world writable
    chmod 600 /etc/sudoers.d/wheel-nopasswd

    # Allow the builder user to run rootless podman
    # Referenced at: https://github.com/containers/podman/issues/4056#issuecomment-1245715492
    # Lifted from: https://github.com/containers/podman/blob/6e382d9ec2e6eb79a72537544341e496368b6c63/contrib/podmanimage/stable/Containerfile#L25-L26
    echo -e "builder:1:999\nbuilder:1001:64535" > /etc/subuid
    echo -e "builder:1:999\nbuilder:1001:64535" > /etc/subgid

}

write_archive_info() {
    # shellcheck source=src/cmdlib.sh
    . "${srcdir}/src/cmdlib.sh"
    mkdir -p /cosa /lib/coreos-assembler
    touch -f /lib/coreos-assembler/.clean
    prepare_git_artifacts "${srcdir}" /cosa/coreos-assembler-git.json /cosa/coreos-assembler-git.tar.gz
}

patch_osbuild() {
    # Add a few patches that either haven't made it into a release or
    # that will be obsoleted with other work that will be done soon.

    # To make it easier to apply patches we'll move around the osbuild
    # code on the system first:
    rmdir /usr/lib/osbuild/osbuild
    mv /usr/lib/python3.12/site-packages/osbuild /usr/lib/osbuild/
    mkdir /usr/lib/osbuild/tools
    mv /usr/bin/osbuild-mpp /usr/lib/osbuild/tools/

    # Now all the software is under the /usr/lib/osbuild dir and we can patch
    patch -d /usr/lib/osbuild -p1 < /usr/lib/coreos-assembler/0001-stages-dmverity-make-device-objects-more-generic.patch

    # And then move the files back; supermin appliance creation will need it back
    # in the places delivered by the RPM.
    mv /usr/lib/osbuild/tools/osbuild-mpp /usr/bin/osbuild-mpp
    mv /usr/lib/osbuild/osbuild /usr/lib/python3.12/site-packages/osbuild
    mkdir /usr/lib/osbuild/osbuild
}

if [ $# -ne 0 ]; then
  # Run the function specified by the calling script
  ${1}
else
  # Otherwise, just run all the steps.  NOTE: This is presently not actually
  # used in `Dockerfile`, so if you add a stage you'll need to do it both
  # here and there.
  configure_yum_repos
  install_rpms
  write_archive_info
  make_and_makeinstall
  install_ocp_tools
  trust_redhat_gpg_keys
  configure_user
  patch_osbuild
fi
