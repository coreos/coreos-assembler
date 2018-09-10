This container aggregates various tools used to build [Fedora CoreOS](https://coreos.fedoraproject.org)
style systems.

It reuses various upstream tools, such as:

 - [mantle](https://github.com/coreos/mantle)
 - [rpm-ostree](https://github.com/projectatomic/rpm-ostree/)
 - [libvirt](https://github.com/libvirt/libvirt)

Getting started - prerequisites
---

You can use `podman` or `docker`. These examples use `podman`. Note the
container must be privileged, as the build process uses container functionality
itself - we're using [recursive containers](https://github.com/projectatomic/bubblewrap/issues/284).

Secondly, in order to build VM images, the container must have access to
`/dev/kvm`.  If you're running this in a VM, you must enable
[nested virt](https://docs.fedoraproject.org/en-US/quick-docs/using-nested-virtualization-in-kvm/).
See also [GCE nested virt](https://cloud.google.com/compute/docs/instances/enable-nested-virtualization-vm-instances).

Setup
---

Here we store data in `/srv/coreos` on our host system.  You can choose
any directory you like.  You should run these commands as `root`.

```
$ mkdir /srv/coreos
$ cd /srv/coreos
$ alias coreos-assembler='podman run --rm --net=host -ti --privileged -v $(pwd):/srv --workdir /srv quay.io/cgwalters/coreos-assembler:testing'
```

Initializing
---

You only need to do this once; it will clone the specified
configuration repo, create various directories and also
download an installer image (used to make VMs).

```
$ coreos-assembler init https://github.com/cgwalters/fedora-coreos-config
```

The specified git repository will be cloned into `/srv/coreos/src/config`.

If you're doing something custom, you likely want to fork that upstream
repository.

Performing a build
---

```
$ coreos-assembler build
```

Each build will write an ostree commit into `/srv/coreos/repo` as well
as generate VM images in `/srv/coreos/builds/`.

Development
---

The container image is built in [OpenShift CI](https://api.ci.openshift.org/console/project/coreos/browse/builds/coreos-assembler?tab=history).
