This container aggregates various tools used to build [Fedora CoreOS](https://coreos.fedoraproject.org)
style systems.

It reuses various upstream tools, such as:

 - [mantle](https://github.com/coreos/mantle)
 - [rpm-ostree](https://github.com/projectatomic/rpm-ostree/)
 - [libvirt](https://github.com/libvirt/libvirt)

A high level goal of this tool is to support two highly related use cases,
and to keep them as similar as possible:

 - Local development ("test a kernel change")
 - Be a production build system orchestrated by an external tool (e.g. Jenkins)

See [fedora-coreos-ci](https://github.com/dustymabe/fedora-coreos-ci) as an
example pipeline.

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
$ alias coreos-assembler='podman run --rm --net=host -ti --privileged --userns=host -v $(pwd):/srv --workdir /srv quay.io/coreos-assembler/coreos-assembler'
```

If you need access to CA certificates on your host (for example, when you need to access
a git repo that is not on the public Internet), you can mount in the host certificates
as read-only.  For example, on a Fedora host the alias would change to:

`$ alias coreos-assembler='podman run --rm --net=host -ti --privileged --userns=host -v /etc/pki:/etc/pki:ro -v $(pwd):/srv --workdir /srv quay.io/coreos-assembler/coreos-assembler'`

See this [Stack Overflow question](https://stackoverflow.com/questions/26028971/docker-container-ssl-certificates) for additional discussion.

Initializing
---

You only need to do this once; it will clone the specified
configuration repo, create various directories and also
download an installer image (used to make VMs).

```
$ coreos-assembler init https://github.com/coreos/fedora-coreos-config
```

The specified git repository will be cloned into `/srv/coreos/src/config`.

If you're doing something custom, you likely want to fork that upstream
repository.

Performing a build
---

First, we fetch all the metadata and packages:

```
$ coreos-assembler fetch
```

And now we can build from these inputs:

```
$ coreos-assembler build
```

Each build will write an OSTree commit into `/srv/coreos/repo` as well
as generate VM images in `/srv/coreos/builds/`.

Next, rerun `coreos-assembler build` and notice the system correctly
deduces that nothing changed.  You can run `coreos-assembler fetch`
again to check for updated RPMs.

Running
---

```
$ coreos-assembler run
```

This invokes QEMU on the image in `builds/latest`.  It uses `-snapshot`,
so any changes are thrown away after you exit qemu.  To exit, type
`Ctrl-a x`.  For more options, type `Ctrl-a ?`.

Making changes
---

Currently, the assembler only takes two input files that are from `src/config`:

 - `manifest.yaml`: An rpm-ostree "manifest" or "treefile", which mostly boils
   down to a list of RPMs and a set of rpm-md repositories
   they come from.  It also supports `postprocess` to make
   arbitrary changes.  See the [upstream docs](https://github.com/projectatomic/rpm-ostree/blob/master/docs/manual/treefile.md).
 - `image.ks`: An [Anaconda  Kickstart](https://pykickstart.readthedocs.io/en/latest/)
   file.  Use this to define the base disk image output.

Let's try editing the file `src/config/image.ks`.  Change the root
storage line `logvol /` for example.  Rerun `coreos-assembler build`, and notice
that the OSTree commit didn't change, but a new image is generated in `builds`.
When you `coreos-assembler run`, you'll get it.

Another thing to try is editing `src/config/manifest.yaml` - add or
remove entries from `packages`.  You can also add local rpm-md `file:///`
repositories.

Development
---

The container image is built in [OpenShift CI](https://api.ci.openshift.org/console/project/rhcos/browse/builds/coreos-assembler?tab=history).
