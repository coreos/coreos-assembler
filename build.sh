#!/usr/bin/env bash
set -euo pipefail

# Detect what platform we are on
if grep -q '^Fedora' /etc/redhat-release; then
    ISFEDORA=1
    ISEL=''
elif grep -q '^Red Hat' /etc/redhat-release; then
    ISFEDORA=''
    ISEL=1
else
    echo 1>&2 "should be on either RHEL or Fedora"
    exit 1
fi

if [ $# -eq 0 ]; then
  echo Usage: "build.sh CMD"
  echo "Supported commands:"
  echo "    configure_user"
  echo "    configure_yum_repos"
  echo "    install_rpms"
  echo "    make_and_makeinstall"
  exit 1
fi

set -x
srcdir=$(pwd)

configure_yum_repos() {
    if [ -n "${ISFEDORA}" ]; then
        # Add FAHC https://pagure.io/fedora-atomic-host-continuous
        # but as disabled.  Today FAHC isn't multi-arch.  But let's
        # add it so that anyone on x86_64 who wants to test the latest
        # ostree/rpm-ostree can easily do so.
        # NOTE: The canonical copy of this code lives in rpm-ostree's CI:
        # https://github.com/projectatomic/rpm-ostree/blob/d2b0e42bfce972406ac69f8e2136c98f22b85fb2/ci/build.sh#L13
        # Please edit there first
        echo -e '[fahc]\nenabled=0\nmetadata_expire=1m\nbaseurl=https://ci.centos.org/artifacts/sig-atomic/fahc/rdgo/build/\ngpgcheck=0\n' > /etc/yum.repos.d/fahc.repo
    fi
}

install_rpms() {
    # First, a general update; this is best practice.  We also hit an issue recently
    # where qemu implicitly depended on an updated libusbx but didn't have a versioned
    # requires https://bugzilla.redhat.com/show_bug.cgi?id=1625641
    yum -y distro-sync

    # xargs is part of findutils, which may not be installed
    yum -y install /usr/bin/xargs

    # define the filter we want to use to filter out deps that don't
    # apply to the platform we are on
    [ -n "${ISFEDORA}" ] && filter='^#FEDORA '
    [ -n "${ISEL}" ]     && filter='^#EL7 '

    # These are only used to build things in here.  Today
    # we ship these in the container too to make it easier
    # to use the container as a development environment for itself.
    # Down the line we may strip these out, or have a separate
    # development version.
    builddeps=$(sed "s/${filter}//" "${srcdir}"/build-deps.txt | grep -v '^#')

    # Process our base dependencies + build dependencies and install
    deps=$(sed "s/${filter}//" "${srcdir}"/deps.txt | grep -v '^#')
    archdeps=$(sed "s/${filter}//" "${srcdir}/deps-$(arch)".txt | grep -v '^#')
    echo "${builddeps}" "${deps}" "${archdeps}" | xargs yum -y install

    # Commented out for now, see above
    #dnf remove -y $builddeps}
    # can't remove grubby on el7 because libguestfs-tools depends on it
    if [ -n "${ISFEDORA}" ]; then
        rpm -q grubby && yum remove -y grubby
    fi

    # Open up permissions on /boot/efi files so we can copy them
    # for our ISO installer image
    find /boot/efi -type f -print0 | xargs -r -0 chmod +r
    find /boot/efi -type d -print0 | xargs -r -0 chmod +rx

    # Further cleanup
    yum clean all

    # shellcheck source=src/cmdlib.sh
    . "${srcdir}/cmdlib.sh"
    depcheck "${deps}"
}

_prep_make_and_make_install() {
    # TODO: install these as e.g.
    # /usr/bin/ostree-releng-script-rsync-repos
    mkdir -p /usr/app/
    rsync -rlv "${srcdir}"/ostree-releng-scripts/ /usr/app/ostree-releng-scripts/

    if [ "$(git submodule status mantle | head -c1)" == "-" ]; then
        echo -e "\033[1merror: submodules not initialized. Run: git submodule update --init\033[0m" 1>&2
        exit 1
    fi

    # Can only (easily) get gobject-introspection in Python2 on EL7
    if [ -n "${ISEL}" ]; then
      sed -i 's|^#!/usr/bin/python3|#!/usr/bin/python2|' src/commitmeta_to_json
      sed -i 's|^#!/usr/bin/env python3|#!/usr/bin/python2|' src/cmd-oscontainer
    fi
}

arch=$(uname -m)
release="29"
# Download url is different for primary and secondary fedora
# Primary Fedora - https://download.fedoraproject.org/pub/fedora/linux/releases/
# Secondary Fedora - https://download.fedoraproject.org/pub/fedora-secondary/releases/
declare -A repository_dirs
repository_dirs[aarch64]=fedora/linux
repository_dirs[armhfp]=fedora/linux
repository_dirs[x86_64]=fedora/linux
repository_dirs[i386]=fedora-secondary
repository_dirs[ppc64le]=fedora-secondary
repository_dirs[s390x]=fedora-secondary

repository_dir=${repository_dirs[$arch]}
INSTALLER=https://download.fedoraproject.org/pub/$repository_dir/releases/$release/Everything/$arch/iso/Fedora-Everything-netinst-$arch-$release-1.2.iso
INSTALLER_CHECKSUM=https://download.fedoraproject.org/pub/$repository_dir/releases/$release/Everything/$arch/iso/Fedora-Everything-$release-1.2-$arch-CHECKSUM

install_anaconda() {
    # Overriding install URL
    if [ -n "${INSTALLER_URL_OVERRIDE-}" ]; then
        INSTALLER="${INSTALLER_URL_OVERRIDE}"
        info "Overriding the install URL with contents of INSTALLER_URL_OVERRIDE"
    fi
    # Overriding install checksum URL
    if [ -n "${INSTALLER_CHECKSUM_URL_OVERRIDE-}" ]; then
        INSTALLER_CHECKSUM="${INSTALLER_CHECKSUM_URL_OVERRIDE}"
        info "Overriding the install checksum URL with contents of INSTALLER_CHECKSUM_URL_OVERRIDE"
    fi

    installer_bn=$(basename "${INSTALLER}")
    checksums_bn=$(basename "${INSTALLER_CHECKSUM}")

    anacondadir=/usr/lib/coreos-assembler-anaconda
    if ! [ -f "${anacondadir}/${installer_bn}" ]; then
        (
            mkdir -p $anacondadir
            cd $anacondadir
            rm tmp -rf && mkdir -p tmp
            cd tmp
            curl -L --remote-name-all "${INSTALLER}" "${INSTALLER_CHECKSUM}"
            sha256sum -c "${checksums_bn}"
            mv "${installer_bn}" "${checksums_bn}" ..
            cd ..
            rmdir tmp
        )
    fi
}

make_and_makeinstall() {
    _prep_make_and_make_install
    # And the main scripts
    if [ -n "${ISEL}" ]; then
        echo "make && make check && make install" | scl enable rh-python36 bash
    else
        make && make check && make install
    fi
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
}

write_archive_info() {
    # shellcheck source=src/cmdlib.sh
    . "${srcdir}/src/cmdlib.sh"
    mkdir -p /cosa /lib/coreos-assembler
    touch -f /lib/coreos-assembler/.clean
    prepare_git_artifacts /root/containerbuild /cosa/coreos-assembler-git.tar.gz /cosa/coreos-assembler-git.json
}

# Run the function specified by the calling script
${1}
