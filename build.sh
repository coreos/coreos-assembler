#!/usr/bin/bash
set -xeuo pipefail

# selinux-policy-targeted is needed for rpm-ostree rojig.
# rsync, python2, pygobject3-base are dependencies of ostree-releng-scripts
# Also add python3 so people can use that too.
# createrepo_c+yum-utils is used for managing rojig bits.
yum -y install rpm-ostree selinux-policy-targeted rpm-build \
    make cargo golang git jq \
    rsync pygobject3-base python3-gobject-base \
    createrepo_c dnf-utils

mkdir -p /usr/app/
cd /usr/app/
git clone https://github.com/ostreedev/ostree-releng-scripts

cd /root/src
ls -al
make build PREFIX=/usr/bin
cargo build --release
mv target/release/coreos-assembler /usr/bin
cd /
rm /root/src -rf

# Part of general image management
yum -y install awscli
cd /root
git clone https://github.com/coreos/mantle
# for now just build ore, we can add more components as we use them
mantle/build ore
mv mantle/bin/ore /usr/bin
rm mantle -rf

dnf remove -y cargo golang
rpm -q grubby && dnf remove -y grubby

# more tooling for building openshift/os in the container
cd /root
git clone https://github.com/openshift/os
cp os/RPM-GPG-KEY* /etc/pki/rpm-gpg/
rm os -rf
dnf copr -y enable walters/buildtools-fedora
dnf -y install dnf-plugins-core fedpkg openssh-clients rpmdistro-gitoverlay
yum clean all
