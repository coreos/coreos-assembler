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
    echo "${builddeps}" "${deps}" | xargs yum -y install

    # grab virt-install from updates testing for now
    # https://bugzilla.redhat.com/show_bug.cgi?id=1659242
    # can delete once https://bodhi.fedoraproject.org/updates/FEDORA-2019-c38a307cd5
    # is stable
    if [ -n "${ISFEDORA}" ]; then
        yum upgrade -y virt-install --enablerepo=updates-testing
    fi

    # Commented out for now, see above
    #dnf remove -y $builddeps}
    # can't remove grubby on el7 because libguestfs-tools depends on it
    if [ -n "${ISFEDORA}" ]; then
        rpm -q grubby && yum remove -y grubby
    fi

    # Further cleanup
    yum clean all
}

_prep_make_and_make_install() {
    # Work around https://github.com/coreos/coreos-assembler/issues/27
    if ! test -d .git; then
        (git config --global user.email dummy@example.com
         git init && git add . && git commit -a -m 'dummy commit'
         git tag -m tag dummy-tag) >/dev/null
    fi

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
