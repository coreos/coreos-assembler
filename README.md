This is the CoreOS Assembler (often abbreviated COSA) build environment. It is 
a collection of various tools used to build 
[Fedora CoreOS](https://coreos.fedoraproject.org) style systems.


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

### Getting started - prerequisites
---

You can use `podman` or `docker`. These examples use `podman`. Note
that we support running in a privileged or unprivileged mode (detailed
below). Regardless of whether you are using privileged or unprivileged
mode you'll need access to `/dev/kvm` as the build process runs a
virtual machine in order to generate the target image. If you're running
this in a VM, you must enable [nested virt](https://docs.fedoraproject.org/en-US/quick-docs/using-nested-virtualization-in-kvm/).
See also [GCE nested virt](https://cloud.google.com/compute/docs/instances/enable-nested-virtualization-vm-instances).

#### Unprivileged Mode

Unprivileged mode is designed to work with very minimal privileges by
doing all "privileged" operations inside of a VM. VMs can be started
as a normal user as long as `/dev/kvm` is accessible (a prerequisite
mentioned above) and thus we are able to perform a compose as a normal
user. This allows us to run inside of a locked down OpenShift
environment, which is where we are running our builds for Fedora
CoreOS currently. 

We recommend you use unprivileged mode when building locally if you
are hacking on Fedora CoreOS so that you can mimic our build
environment as much as possible.

#### Privileged Mode

In privileged mode the rpm-ostree compose uses container features
itself, thus requires a privileged container in order to perform the
compose. This is known as [recursive containers](https://github.com/projectatomic/bubblewrap/issues/284).

### Container Build
---

To completely rebuild the COSA container image locally, execute
`$ sudo podman build -t localhost/coreos-assembler .` or
`$ sudo podman build -t localhost/coreos-assembler -f Dockerfile.rhel .`
from the `coreos-assembler` repository. If building the RHEL version please 
note you will need a maipo directory with references to
proper repositories.


### Setup
---

Let's set up our working directory first. We'll create and use `./srv-coreos`
on our host system. You can choose any directory you like. 

```
$ mkdir ./srv-coreos
$ setfacl -m u:1000:rwx ./srv-coreos
$ setfacl -d -m u:1000:rwx ./srv-coreos
$ chcon system_u:object_r:container_file_t:s0 ./srv-coreos
$ cd ./srv-coreos
```

In the above commands we:

- created the `./srv-coreos` directory
- set file ACLs so that the `builder` user (uid `1000` inside the container) can write files
- set file ACLs so that new files get created with ACLs that allow the `builder` user
- gave the directory an SELinux file context for sharing with containers
- changed our working directory into `./srv-coreos`

Now we'll define a bash function that we can use to call the assembler
container:

```
$ cosa() {
    env | grep COREOS_ASSEMBLER
    set -x # so we can see what command gets run
    sudo podman run --rm -ti -v ${PWD}:/srv/ --userns=host --device /dev/kvm --name cosa \
               ${COREOS_ASSEMBLER_PRIVILEGED:+--privileged}                                          \
               ${COREOS_ASSEMBLER_CONFIG_GIT:+-v $COREOS_ASSEMBLER_CONFIG_GIT:/srv/src/config/:ro}   \
               ${COREOS_ASSEMBLER_GIT:+-v $COREOS_ASSEMBLER_GIT/src/:/usr/lib/coreos-assembler/:ro}  \
               ${COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS}                                            \
               ${COREOS_ASSEMBLER_CONTAINER:-quay.io/coreos-assembler/coreos-assembler:latest} $@
    rc=$?; set +x; return $rc
}
```

**NOTE**: We're using `cosa` here as it is much easier to type than `coreos-assembler`.

This is a bit more complicated than a simple alias, but it allows for
hacking on the assembler or the configs and prints out the environment and
the command that ultimately gets run. Let's step through each part:

- `sudo podman run --rm -ti`: standard container invocation
- `-v ${PWD}:/srv/`: mount local working dir under `/srv/` in container
- `--userns=host`: the default for podman anyway, but required for docker
- `--device /dev/kvm`: needed for creating VMs
- `--name cosa`: just a name, feel free to change it


The environment variables are special purpose:


- `COREOS_ASSEMBLER_PRIVILEGED`

Setting `COREOS_ASSEMBLER_PRIVILEGED=true` (or any value) will cause
`--privileged` to get added on the command line.

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


### Initializing
---

You only need to do this once; it will clone the specified
configuration repo, create various directories and also
download an installer image (used to make VMs).

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

Each build will write an OSTree commit into `$PWD/repo/` as well
as generate VM images in `$PWD/builds/`.

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

#### Hacking on CoreOS Git Configs

We can hack on some local input configs by exporting them in the
`COREOS_ASSEMBLER_CONFIG_GIT` env variable. For example:

```
$ export COREOS_ASSEMBLER_CONFIG_GIT=/path/to/github.com/coreos/fedora-coreos-config/
$ cosa init --force /dev/null
$ cosa fetch && cosa build
```

Currently, the assembler only takes two input files that are from `src/config`:

 - `manifest.yaml`: An rpm-ostree "manifest" or "treefile", which mostly boils
   down to a list of RPMs and a set of rpm-md repositories
   they come from.  It also supports `postprocess` to make
   arbitrary changes.  See the [upstream docs](https://github.com/projectatomic/rpm-ostree/blob/master/docs/manual/treefile.md).
 - `image.ks`: An [Anaconda  Kickstart](https://pykickstart.readthedocs.io/en/latest/)
   file.  Use this to define the base disk image output.

Let's try editing the file `src/config/image.ks`.  Change the root
storage line `logvol /` for example.  Rerun `cosa build`, and notice
that the OSTree commit didn't change, but a new image is generated in `builds`.
When you `cosa run`, you'll get it.

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

#### Running in privileged mode

If you'd like to run the Assembler in [privileged mode](#privileged-mode)
you can use the `COREOS_ASSEMBLER_PRIVILEGED` env var:

```
$ export COREOS_ASSEMBLER_PRIVILEGED=true
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

#### Overriding RPMs: Using new, different, or locally-built RPMs

To override the RPM packages requested in `src/config/manifest.yaml`,
drop local RPM packages into a directory `overrides/rpm`. This will
generate a `coreos-assembler-local-overrides` repository during the
next build where the overriding packages will be pulled from. Then, run
`cosa build` to rebuild with the local overrides.

As an example, from your assembler directory:

```
$ mkdir -p overrides/rpm
$ cp /path/to/my/name-version-release.rpm ./overrides/rpm
$ cosa build
```
