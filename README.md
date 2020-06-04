The CoreOS Assembler
====

This is the CoreOS Assembler (often abbreviated COSA) build environment. It is
a collection of various tools used to build
[Fedora CoreOS](https://coreos.fedoraproject.org) style systems.  You
can use this to create Ignition + OSTree based operating systems with
custom components and manage updates yourself, etc.

It reuses various upstream tools, such as:

- [mantle](https://github.com/coreos/mantle)
- [rpm-ostree](https://github.com/projectatomic/rpm-ostree/)
- [libvirt](https://github.com/libvirt/libvirt)

A high level goal of this tool is to support two highly related use cases,
and to keep them as similar as possible:

- Local development ("test a kernel change")
- Be a production build system orchestrated by an external tool (e.g. Jenkins)

See [fedora-coreos-pipeline](https://github.com/coreos/fedora-coreos-pipeline) as an
example pipeline.

The container itself is available on Quay.io at
`quay.io/coreos-assembler/coreos-assembler`.

## Design

See [README-design.md](README-design.md) for more information about the overall design.

## Development
For development information, including how to add new tests, please see [README-devel.md](README-devel.md).

### Build Process

<img src="./build-process.svg?sanitize=true">

See [update build process svg](build-process-mermaid.md)

### Getting started - prerequisites
---

The default instructions here use `podman`, but it is also possible to use
`docker`.

However, the most important prerequisite to understand is that
coreos-assembler generates disk images, and creating those in a
maintainable way requires access to virtualization - specifically
`/dev/kvm`.

If you're running in a local KVM VM, you can try enabling [nested virt](https://docs.fedoraproject.org/en-US/quick-docs/using-nested-virtualization-in-kvm/).

There are various public cloud options that provide either bare metal or nested virt, such as:

- [Packet](https://www.packet.com/)
- [GCE nested virt](https://cloud.google.com/compute/docs/instances/enable-nested-virtualization-vm-instances)
- EC2 `i3.metal` instances
- [IBM Bare Metal](https://www.ibm.com/cloud/bare-metal-servers)

etc.

Further, it is fully supported to run coreos-assembler inside Kubernetes;
the Fedora CoreOS pipeline runs it inside OpenShift as an unprivileged pod
on a bare metal cluster, with `/dev/kvm` mounted in.  See the
[Fedora CoreOS pipeline](https://github.com/coreos/fedora-coreos-pipeline)
source code.

See also a [guide to using nested virt with OpenShift in GCP](doc/openshift-gcp-nested-virt.md).

### Downloading the container

```
$ podman pull quay.io/coreos-assembler/coreos-assembler
```

### Create a build working directory
---

coreos-assembler operates on a "build directory" which should be
the current working directory.  This is much like how `git` works
on a git repository.

We'll create and use `./fcos` on our host system.
You can choose any directory you like.

```
$ mkdir fcos
$ cd fcos
```

### Define a bash alias to run cosa

Now we'll define a bash function that we can use to call the assembler
container.  There are a number of tweaks here on top of base `podman`.

Note: *You should run this command as non-root*

It's also fully supported to use `podman` as root, but some of the arguments
here need to change for that.

```
cosa() {
   env | grep COREOS_ASSEMBLER
   set -x
   podman run --rm -ti --security-opt label=disable --privileged                                    \
              --uidmap=1000:0:1 --uidmap=0:1:1000 --uidmap 1001:1001:64536                          \
              -v ${PWD}:/srv/ --device /dev/kvm --device /dev/fuse                                  \
              --tmpfs /tmp -v /var/tmp:/var/tmp --name cosa                                         \
              ${COREOS_ASSEMBLER_CONFIG_GIT:+-v $COREOS_ASSEMBLER_CONFIG_GIT:/srv/src/config/:ro}   \
              ${COREOS_ASSEMBLER_GIT:+-v $COREOS_ASSEMBLER_GIT/src/:/usr/lib/coreos-assembler/:ro}  \
              ${COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS}                                            \
              ${COREOS_ASSEMBLER_CONTAINER:-quay.io/coreos-assembler/coreos-assembler:latest} "$@"
   rc=$?; set +x; return $rc
}
```

This is a bit more complicated than a simple alias, but it allows for
hacking on the assembler or the configs and prints out the environment and
the command that ultimately gets run. Let's step through each part:

- `podman run --rm -ti`: standard container invocation
- `--privileged`: Note we're running as non root, so this is still safe (from the host's perspective)
- `--security-opt label:disable`: Disable SELinux isolation so we don't need to relabel the build directory
- `--uidmap=1000:0:1 --uidmap=0:1:1000 --uidmap 1001:1001:64536`: map the `builder` user to root in the user namespace where root in the user namespace is mapped to the calling user from the host. See [this well formatted explanation](https://github.com/debarshiray/toolbox/commit/cfcf4eb31e14b3a300804840d315c62bc32e15ae) of the complexities of user namespaces in rootless podman.
- `--device /dev/kvm --device /dev/fuse`: Bind in necessary devices
- `--tmpfs`: We want /tmp to go away when the container restarts; it's part of the "ABI" of /tmp
- `-v /var/tmp:/var/tmp`: Some cosa commands may allocate larger temporary files (e.g. supermin; forward this to the host)
- `-v ${PWD}:/srv/`: mount local working dir under `/srv/` in container
- `--name cosa`: just a name, feel free to change it

The environment variables are special purpose:

- `COREOS_ASSEMBLER_CONFIG_GIT`

Allows you to specifiy a local directory that contains the configs for
the ostree you are trying to compose.

- `COREOS_ASSEMBLER_GIT`

Allows you to specify a local directory that contains the CoreOS
Assembler scripts. This allows for quick hacking on the assembler
itself.

- `COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS`

Allows for adding arbitrary mounts or args to the container runtime.

- `COREOS_ASSEMBLER_CONTAINER`

Allows for overriding the default assembler container which is
currently `quay.io/coreos-assembler/coreos-assembler:latest`.

See the [Hacking](#hacking) section below for examples of how to use these
variables:

### Running persistently

At this point, try `cosa shell` to start a shell inside the container.
From here, you can run `cosa ...` to invoke build commands.

However, you can also choose to run the `cosa` bash alias above
and create a transient new container for each command.  Either
way, all data persists in the build directory.

### Running multiple instances

Remove `--name cosa` from the `cosa` alias to let `podman` pick a random name
for the container and allow multiple instances of the `cosa` container to run
simultaneously. This is not the default to avoid hard to debug issues but should
be safe for running multiple instances of `kola` with different builds for
example.

### Initializing
---

You only need to do this once; it will clone the specified
configuration repo, and create various directories/state such
as the OSTree repository.  (For production, you will want to
sync this repository out so clients can get updates).

```
$ cosa init https://github.com/coreos/fedora-coreos-config
```

The specified git repository will be cloned into `$PWD/src/config/`.

If you're doing something custom, you likely want to fork that upstream
repository.

### Performing a build
---

First, we fetch all the metadata and packages:

```
$ cosa fetch
```

And now we can build from these inputs:

```
$ cosa build
```

Each build will create a new directory in `$PWD/builds/`, containing the
generated OSTree commit (as a tarball) and the qemu VM image.

Next, rerun `cosa build` and notice the system correctly
deduces that nothing changed.  You can run `cosa fetch`
again to check for updated RPMs.

### Running
---

```
$ cosa run
```

This invokes QEMU on the image in `builds/latest`.  It uses `-snapshot`,
so any changes are thrown away after you exit qemu.  To exit, type
`Ctrl-a x`.  For more options, type `Ctrl-a ?`.


### Hacking
---

#### Building the cosa container image locally
---

To completely rebuild the COSA container image locally, use e.g.
`$ podman build -t localhost/coreos-assembler .`  You should then set
`COREOS_ASSEMBLER_CONTAINER=localhost/coreos-assembler` in the environment
if you're using the bash alias `cosa`.

#### Installing cosa inside an existing container
---

If you already have a pet container you'd like to keep using that
matches the same Fedora release cosa uses, you can install cosa inside
of it by doing:

```
sudo ./build.sh configure_yum_repos
sudo ./build.sh install_rpms
make
sudo make install
```

From that point on, you only need to run `make && sudo make install` if
you're hacking on cosa itself (unless there are new RPM requirements
added).

#### Understanding "config git"

Conceptually, coreos-assembler ties together generating OSTree commits with disk images
into a single "build schema".  The build target is defined by the "config git"
which should be in `src/config` (relative to the build directory).

We can hack on some local input configs by exporting them in the
`COREOS_ASSEMBLER_CONFIG_GIT` env variable. For example:

```
$ export COREOS_ASSEMBLER_CONFIG_GIT=/path/to/github.com/coreos/fedora-coreos-config/
$ cosa init --force /dev/null
$ cosa fetch && cosa build
```

#### Components of "config git"

`manifest.yaml`
----

For generating OSTree commits, cosa uses `manifest.yaml`: An rpm-ostree "manifest" or "treefile", which mostly boils
down to a list of RPMs and a set of rpm-md repositories
they come from.  It also supports `postprocess` to make
arbitrary changes.  See the [upstream docs](https://github.com/projectatomic/rpm-ostree/blob/master/docs/manual/treefile.md).

`overlay.d/`
----

coreos-assembler also supports a generic way to embed architecture-independent
configuration and scripts by creating subdirectories in `overlay.d/`.  Each subdirectory
of the `overlay.d.` directory is added to the OSTree commit, in lexicographic order.
It's recommended to name directories with a numeric prefix - e.g. `05core`, `10extras`.
Non-directories are ignored.  For example, a good practice is to add a `README.md` file
into your overlay directories describing their structure.

`image.yaml`
----

This YAML file configures the output disk images.  Supported keys are:

 - `size`: Size in GB for cloud images (OpenStack, AWS, etc.)  Required.
 - `extra-kargs`: List of kernel arguments.

It's likely in the future we will extend this to support e.g. a separate `/var`
partition or configuring the filesystem types.  If you want to do anything like
that today it requires forking the assembler and rebuilding it.
See the fedora-coreos-config for an example.

#### Hacking on config git

First you can expand the size of the image; edit `src/config/image.yaml` and
e.g. change `8` to `9`. Rerun `cosa build`, and notice that the OSTree commit
didn't change, but a new image is generated in `builds`. When you `cosa run`,
you'll get it.

Another thing to try is editing `src/config/manifest.yaml` - add or
remove entries from `packages`.  You can also add local rpm-md `file:///`
repositories.

#### Hacking on CoreOS Assembler Scripts

If you find yourself wanting to hack on CoreOS Assembler itself then
you can easily mount the scripts into the container and prevent
rebuilding the container to test every change. This can be done using
the `COREOS_ASSEMBLER_GIT` env var.

```
$ export COREOS_ASSEMBLER_GIT=/path/to/github.com/coreos/coreos-assembler/
$ cosa init https://github.com/coreos/fedora-coreos-config.git
$ cosa fetch && cosa build
```

#### Using a locally built Assembler container

If you have [built a local assembler container](#container-build)
you can tell the container runtime to use it instead of the default
by setting the `COREOS_ASSEMBLER_CONTAINER` env var:

```
$ export COREOS_ASSEMBLER_CONTAINER=localhost/coreos-assembler
$ cosa init https://github.com/coreos/fedora-coreos-config.git
$ cosa fetch && cosa build
```

#### Using different CA certificates

If you need access to CA certificates on your host (for example, when you need to access
a git repo that is not on the public Internet), you can mount in the host certificates
using the `COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS` variable.

**NOTE** Sharing the `/etc/pki/ca-trust` directory may be blocked by SELinux so you may
         have to use a directory with the `system_u:object_r:container_file_t:s0` file context.

```
$ export COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS='-v /etc/pki/ca-trust:/etc/pki/ca-trust:ro'
$ cosa init https://github.com/coreos/fedora-coreos-config.git
$ cosa fetch && cosa build
```
See this [Stack Overflow question](https://stackoverflow.com/questions/26028971/docker-container-ssl-certificates) for additional discussion.

#### Overriding content and testing component patches

See [README-devel.md](README-devel.md#using-overrides).

### Pulling in fixed packages into the COSA container

To pull in fixed packages before they make it through Bodhi,
you can simply tag them into the
`f${releasever}-coreos-continuous` tag and trigger a
rebuild.
