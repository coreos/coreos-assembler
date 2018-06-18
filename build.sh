#!/usr/bin/bash
set -xeuo pipefail

# selinux-policy-targeted is needed for rpm-ostree rojig.
# rsync, python2, pygobject3-base are dependencies of ostree-releng-scripts
# Also add python3 so people can use that too.
# createrepo_c+yum-utils is used for managing rojig bits.
dnf -y install rpm-ostree selinux-policy-targeted rpm-build \
    make cargo golang git jq \
    rsync pygobject3-base python3-gobject-base \
    createrepo_c dnf-utils

# Gather RPMs before we ran builddep
rpm -qa --queryformat='%{NAME}\n' | sort -u > /root/rpms.txt
dnf builddep -y rpm-ostree
git clone https://github.com/projectatomic/rpm-ostree
cd rpm-ostree
# Note --enable-rust
./autogen.sh --prefix=/usr --libdir=/usr/lib64 --sysconfdir=/etc --enable-rust
make -j 8
make install
cd ..
rm rpm-ostree -rf
rpm -qa --queryformat='%{NAME}\n' |sort -u > /root/rpms-new.txt
# Yeah this is a pretty awesome hack; now we remove the BuildRequires
comm -1 -3 /root/rpms{,-new}.txt | xargs -r yum -y remove

mkdir -p /usr/app/
cd /usr/app/
git clone https://github.com/ostreedev/ostree-releng-scripts

mv /root/src/coreos-assembler.sh /usr/bin/coreos-assembler
rm /root/src -rf

# Part of general image management
dnf -y install awscli
cd /root
git clone https://github.com/coreos/mantle
# for now just build ore, we can add more components as we use them
mantle/build ore
mv mantle/bin/ore /usr/bin
rm mantle -rf

dnf remove -y cargo golang
rpm -q grubby && dnf remove -y grubby

# more tooling for building openshift/os in the container
dnf copr -y enable walters/buildtools-fedora
dnf -y install dnf-plugins-core fedpkg openssh-clients rpmdistro-gitoverlay
dnf clean all
