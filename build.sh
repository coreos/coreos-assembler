#!/usr/bin/bash
set -xeuo pipefail

# We want to run what builds we can as an unprivileged user;
# running as non-root is much better for the libvirt stack in particular
# for the cases where we have --privileged in the container run for other reasons.
# At some point we may make this the default.
useradd builder

dnf -y install dnf-utils dnf-plugins-core
dnf copr -y enable walters/buildtools-fedora

# Pull latest rpm-ostree
curl -L --remote-name-all https://kojipkgs.fedoraproject.org//packages/rpm-ostree/2018.7/1.fc28/x86_64/rpm-ostree-{,libs-}2018.7-1.fc28.x86_64.rpm
dnf -y install ./rpm-ostree*.rpm && rm -f *.rpm

# These are only used to build things in here, we define them separately because
# they're cleaned up later
self_builddeps="cargo golang"

grep -v '^#' <<EOF | xargs dnf -y install
${self_builddeps}

# dumb-init is a good idea in general, but specifically fixes things with
# libvirt forking qemu and assuming the process gets reaped on shutdown.
dumb-init

# rpmdistro-gitoverlay deps
dnf-plugins-core createrepo_c dnf-utils fedpkg openssh-clients rpmdistro-gitoverlay

# We need these for rojig
selinux-policy-targeted rpm-build

# Standard build tools
make git rpm-build

# virt-install dependencies
libvirt libguestfs-tools qemu-kvm /usr/bin/qemu-img /usr/bin/virsh /usr/bin/virt-install

# ostree-releng-scripts dependencies
rsync pygobject3-base python3-gobject-base

# To support recursive containerization and manipulating images
podman buildah skopeo

# Miscellaneous tools
jq awscli
EOF


mkdir -p /usr/app/
cd /usr/app/
git clone https://github.com/ostreedev/ostree-releng-scripts

cd /root/src
make install
cd /
rm /root/src -rf

# Part of general image management
cd /root
git clone https://github.com/coreos/mantle
cd mantle
# Add components as necessary
./build ore kola kolet
for x in ore kola; do
    mv bin/${x} /usr/bin
done
install -D -m 0755 -t /usr/lib/kola/amd64 bin/amd64/kolet
cd ..
rm mantle -rf

dnf remove -y ${self_builddeps}
rpm -q grubby && dnf remove -y grubby

dnf clean all
