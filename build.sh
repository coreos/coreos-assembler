#!/usr/bin/bash
set -xeuo pipefail

srcdir=$(pwd)

# Enable FAHC https://pagure.io/fedora-atomic-host-continuous
# so we have ostree/rpm-ostree git master for our :latest
# NOTE: The canonical copy of this code lives in rpm-ostree's CI:
# https://github.com/projectatomic/rpm-ostree/blob/d2b0e42bfce972406ac69f8e2136c98f22b85fb2/ci/build.sh#L13
# Please edit there first
echo -e '[fahc]\nmetadata_expire=1m\nbaseurl=https://ci.centos.org/artifacts/sig-atomic/fahc/rdgo/build/\ngpgcheck=0\n' > /etc/yum.repos.d/fahc.repo
# Until we fix https://github.com/rpm-software-management/libdnf/pull/149
excludes='exclude=ostree ostree-libs ostree-grub2 rpm-ostree'
for repo in /etc/yum.repos.d/fedora*.repo; do
    cat ${repo} | (while read line; do if echo "$line" | grep -qE -e '^enabled=1'; then echo "${excludes}"; fi; echo $line; done) > ${repo}.new
    mv ${repo}.new ${repo}
done

# Work around https://github.com/coreos/coreos-assembler/issues/27
if ! test -d .git; then
    dnf -y install git
    (git config --global user.email dummy@example.com
     git init && git add . && git commit -a -m 'dummy commit'
     git tag -m tag dummy-tag) >/dev/null
fi

if ! test -f mantle/README.md; then
    echo "Run: git submodule update --init" 1>&2
    exit 1
fi

# xargs is part of findutils, which may not be installed
# And we want the copr command (for now)
dnf -y install /usr/bin/xargs dnf-utils dnf-plugins-core
dnf copr -y enable walters/buildtools-fedora

# For now, since we get builds slightly faster there
dnf copr -y enable dustymabe/ignition

# These are only used to build things in here, we define them separately because
# they're cleaned up later
self_builddeps="cargo golang"

# Process our base dependencies + build dependencies
(echo ${self_builddeps} && grep -v '^#' ${srcdir}/deps.txt) | xargs dnf -y install

# The podman change to use systemd for cgroups broke our hack to use
# podman-in-docker...we should fix our pipeline, but for now:
dnf -y downgrade https://kojipkgs.fedoraproject.org//packages/podman/0.7.4/4.git80612fb.fc28/x86_64/podman-0.7.4-4.git80612fb.fc28.x86_64.rpm

# TODO: install these as e.g.
# /usr/bin/ostree-releng-script-rsync-repos
mkdir -p /usr/app/
rsync -rlv ${srcdir}/ostree-releng-scripts/ /usr/app/ostree-releng-scripts/

# And the main scripts
make && make install

# Cleanup deps
dnf remove -y ${self_builddeps}
rpm -q grubby && dnf remove -y grubby
# Further cleanup
dnf clean all
cd /
rm ${srcdir} -rf

# We want to run what builds we can as an unprivileged user;
# running as non-root is much better for the libvirt stack in particular
# for the cases where we have --privileged in the container run for other reasons.
# At some point we may make this the default.
useradd builder -G wheel
echo '%wheel ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers.d/wheel-nopasswd
