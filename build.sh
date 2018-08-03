#!/usr/bin/bash
set -xeuo pipefail

# We want to run what builds we can as an unprivileged user;
# running as non-root is much better for the libvirt stack in particular
# for the cases where we have --privileged in the container run for other reasons.
# At some point we may make this the default.
useradd builder

# dumb-init is a good idea in general, but specifically fixes things with
# libvirt forking qemu and assuming the process gets reaped on shutdown.
# selinux-policy-targeted is needed for rpm-ostree rojig.
# rsync, python2, pygobject3-base are dependencies of ostree-releng-scripts
# Also add python3 so people can use that too.
# qemu-img and virsh are for coreos-virt-install.
# createrepo_c+yum-utils is used for managing rojig bits.
# We also install podman+buildah+skopeo to support recursive containerization,
# and manipulating images.
dnf -y install dumb-init \
    rpm-ostree selinux-policy-targeted rpm-build \
    make cargo golang git jq \
    rsync pygobject3-base python3-gobject-base \
    libvirt libguestfs-tools qemu-kvm /usr/bin/qemu-img /usr/bin/virsh /usr/bin/virt-install \
    createrepo_c dnf-utils \
    podman buildah skopeo

# Gather RPMs before we ran builddep
rpm -qa --queryformat='%{NAME}\n' | sort -u > /root/rpms.txt
dnf builddep -y rpm-ostree
# Temporary bits until https://github.com/projectatomic/rpm-ostree/pull/1460
# propagates
dnf -y install python3-sphinx python3-devel
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

cd /root/src
make install
cd /
rm /root/src -rf

# Part of general image management
dnf -y install awscli
cd /root
# We want https://github.com/coreos/mantle/pull/888
git clone --branch rhcos_general https://github.com/arithx/mantle
cd mantle
# Add components as necessary
./build ore kola kolet
for x in ore kola; do
    mv bin/${x} /usr/bin
done
install -D -m 0755 -t /usr/lib/kola/amd64 bin/amd64/kolet
cd ..
rm mantle -rf

dnf remove -y cargo golang
rpm -q grubby && dnf remove -y grubby

# more tooling for building openshift/os in the container
dnf copr -y enable walters/buildtools-fedora
dnf -y install dnf-plugins-core fedpkg openssh-clients rpmdistro-gitoverlay
dnf clean all
