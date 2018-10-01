#!/usr/bin/bash
set -xeuo pipefail

srcdir=$(pwd)

configure_yum_repos() {

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

    # enable `walters/buildtools-fedora` copr
	# pulled from https://copr.fedorainfracloud.org/coprs/walters/buildtools-fedora/repo/fedora-28/walters-buildtools-fedora-fedora-28.repo
    cat > /etc/yum.repos.d/walters-buildtools-fedora-fedora-28.repo  <<'EOF'
[walters-buildtools-fedora]
name=Copr repo for buildtools-fedora owned by walters
baseurl=https://copr-be.cloud.fedoraproject.org/results/walters/buildtools-fedora/fedora-$releasever-$basearch/
type=rpm-md
skip_if_unavailable=True
gpgcheck=1
gpgkey=https://copr-be.cloud.fedoraproject.org/results/walters/buildtools-fedora/pubkey.gpg
repo_gpgcheck=0
enabled=1
enabled_metadata=1
EOF

    # enable `dustymabe/ignition` copr
	# pulled from https://copr.fedorainfracloud.org/coprs/dustymabe/ignition/repo/fedora-28/dustymabe-ignition-fedora-28.repo
    cat > /etc/yum.repos.d/dustymabe-ignition-fedora-28.repo <<'EOF'
[dustymabe-ignition]
name=Copr repo for ignition owned by dustymabe
baseurl=https://copr-be.cloud.fedoraproject.org/results/dustymabe/ignition/fedora-$releasever-$basearch/
type=rpm-md
skip_if_unavailable=True
gpgcheck=1
gpgkey=https://copr-be.cloud.fedoraproject.org/results/dustymabe/ignition/pubkey.gpg
repo_gpgcheck=0
enabled=1
enabled_metadata=1
EOF

}

install_rpms() {

    # First, a general update; this is best practice.  We also hit an issue recently
    # where qemu implicitly depended on an updated libusbx but didn't have a versioned
    # requires https://bugzilla.redhat.com/show_bug.cgi?id=1625641
    dnf -y distro-sync

    # xargs is part of findutils, which may not be installed
    dnf -y install /usr/bin/xargs

    # These are only used to build things in here.  Today
    # we ship these in the container too to make it easier
    # to use the container as a development environment for itself.
    # Down the line we may strip these out, or have a separate
    # development version.
    self_builddeps=$(grep -v '^#' ${srcdir}/build-deps.txt)

    # Process our base dependencies + build dependencies
    (echo ${self_builddeps} && grep -v '^#' ${srcdir}/deps.txt) | xargs dnf -y install

    # The podman change to use systemd for cgroups broke our hack to use
    # podman-in-docker...we should fix our pipeline, but for now:
    dnf -y downgrade https://kojipkgs.fedoraproject.org//packages/podman/0.7.4/4.git80612fb.fc28/x86_64/podman-0.7.4-4.git80612fb.fc28.x86_64.rpm

    # Commented out for now, see above
    #dnf remove -y ${self_builddeps}
    rpm -q grubby && dnf remove -y grubby
    # Further cleanup
    dnf clean all

}

make_and_makeinstall() {

    # Work around https://github.com/coreos/coreos-assembler/issues/27
    if ! test -d .git; then
        (git config --global user.email dummy@example.com
         git init && git add . && git commit -a -m 'dummy commit'
         git tag -m tag dummy-tag) >/dev/null
    fi

    # TODO: install these as e.g.
    # /usr/bin/ostree-releng-script-rsync-repos
    mkdir -p /usr/app/
    rsync -rlv ${srcdir}/ostree-releng-scripts/ /usr/app/ostree-releng-scripts/

    if ! test -f mantle/README.md; then
        echo "Run: git submodule update --init" 1>&2
        exit 1
    fi

    # And the main scripts
    make && make install
}

configure_user(){

    # We want to run what builds we can as an unprivileged user;
    # running as non-root is much better for the libvirt stack in particular
    # for the cases where we have --privileged in the container run for other reasons.
    # At some point we may make this the default.
    useradd builder -G wheel
    echo '%wheel ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers.d/wheel-nopasswd
}

# Run the function specified by the calling script
${1}
