#!/usr/bin/bash
set -xeuo pipefail

srcdir=$(pwd)

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

# We want to run what builds we can as an unprivileged user;
# running as non-root is much better for the libvirt stack in particular
# for the cases where we have --privileged in the container run for other reasons.
# At some point we may make this the default.
useradd builder

# https://bugzilla.redhat.com/show_bug.cgi?id=1625641
dnf -y distro-sync

dnf -y install /usr/bin/xargs dnf-utils dnf-plugins-core
dnf copr -y enable walters/buildtools-fedora

# These are only used to build things in here, we define them separately because
# they're cleaned up later
self_builddeps="cargo golang"

grep -v '^#' <<EOF | xargs dnf -y install
${self_builddeps}

# dumb-init is a good idea in general, but specifically fixes things with
# libvirt forking qemu and assuming the process gets reaped on shutdown.
dumb-init

# rpm-ostree
rpm-ostree

# rpmdistro-gitoverlay deps
dnf-plugins-core createrepo_c dnf-utils fedpkg openssh-clients rpmdistro-gitoverlay

# Currently a transitive req of rpmdistro-gitoverlay via mock, but we
# expect people to use these explicitly in their repo configurations.
distribution-gpg-keys
# We need these for rojig
selinux-policy-targeted rpm-build

# Standard build tools
make git rpm-build

# virt-install dependencies
libvirt libguestfs-tools qemu-kvm /usr/bin/qemu-img /usr/bin/virsh /usr/bin/virt-install
# And we process kickstarts
/usr/bin/ksflatten

# ostree-releng-scripts dependencies
rsync pygobject3-base python3-gobject-base

# To support recursive containerization and manipulating images
podman buildah skopeo

# Miscellaneous tools
jq awscli
EOF

# The podman change to use systemd for cgroups broke our hack to use
# podman-in-docker...we should fix our pipeline, but for now:
dnf -y downgrade https://kojipkgs.fedoraproject.org//packages/podman/0.7.4/4.git80612fb.fc28/x86_64/podman-0.7.4-4.git80612fb.fc28.x86_64.rpm

# TODO: install these as e.g.
# /usr/bin/ostree-releng-script-rsync-repos
mkdir -p /usr/app/
rsync -rlv ${srcdir}/ostree-releng-scripts/ /usr/app/ostree-releng-scripts/

# And the main scripts
make install

# Part of general image management
(cd mantle
 # Add components as necessary
 ./build ore kola kolet
 for x in ore kola; do
     mv bin/${x} /usr/bin
 done
install -D -m 0755 -t /usr/lib/kola/amd64 bin/amd64/kolet
)

# Cleanup deps
dnf remove -y ${self_builddeps}
rpm -q grubby && dnf remove -y grubby
# Further cleanup
dnf clean all
cd /
rm ${srcdir} -rf
