---
nav_order: 2
---

# Building Fedora CoreOS
{: .no_toc }

1. TOC
{:toc}

## Build Process

The following schema summarize the build process used with COSA to build Fedora
CoreOS:

<!-- The path to this SVG is fixed here to make sure that it is correctly
displayed both on the coreos.github.io website rendered by Jekyll and on
github.com while browsing the repository view. The main downside here is that
this will not be updated for branches -->
<img src="https://coreos.github.io/coreos-assembler/build-process.svg?sanitize=true">

See [Updating the build process SVG](build-process-mermaid.md) to update this
schema.

## Getting started - prerequisites

The default instructions here use `podman`, but it is also possible to use
`docker`.

However, the most important prerequisite to understand is that coreos-assembler
generates disk images, and creating those in a maintainable way requires access
to virtualization - specifically `/dev/kvm`.

If you're running in a local KVM VM, you can try enabling [nested virt](https://docs.fedoraproject.org/en-US/quick-docs/using-nested-virtualization-in-kvm/).

There are various public cloud options that provide either bare metal or nested
virt, such as:

- [Packet](https://www.packet.com/)
- [GCE nested virt](https://cloud.google.com/compute/docs/instances/enable-nested-virtualization-vm-instances)
- EC2 `i3.metal` instances
- [IBM Bare Metal](https://www.ibm.com/cloud/bare-metal-servers)
- etc.

Further, it is fully supported to run coreos-assembler inside Kubernetes; the
Fedora CoreOS pipeline runs it inside OpenShift as an unprivileged pod on a
bare metal cluster, with `/dev/kvm` mounted in.  See the [Fedora CoreOS
pipeline](https://github.com/coreos/fedora-coreos-pipeline) source code. See
also the [guide to run COSA using nested virt with OpenShift in GCP](working.md#running-cosa-in-openshift-on-google-compute-platform).

## Downloading the container

```
$ podman pull quay.io/coreos-assembler/coreos-assembler
```

## Create a build working directory

coreos-assembler operates on a "build directory" which should be
the current working directory.  This is much like how `git` works
on a git repository.

We'll create and use `./fcos` on our host system.
You can choose any directory you like.

```
$ mkdir fcos
$ cd fcos
```

## Define a bash alias to run cosa

Now we'll define a bash function that we can use to call the assembler
container.  There are a number of tweaks here on top of base `podman`.

Note: *You should run this command as non-root*

It's also fully supported to use `podman` as root, but some of the arguments
here need to change for that.

{% raw %}
```sh
cosa() {
   env | grep COREOS_ASSEMBLER
   local -r COREOS_ASSEMBLER_CONTAINER_LATEST="quay.io/coreos-assembler/coreos-assembler:latest"
   if [[ -z ${COREOS_ASSEMBLER_CONTAINER} ]] && $(podman image exists ${COREOS_ASSEMBLER_CONTAINER_LATEST}); then
       local -r cosa_build_date_str="$(podman inspect -f "{{.Created}}" ${COREOS_ASSEMBLER_CONTAINER_LATEST} | awk '{print $1}')"
       local -r cosa_build_date="$(date -d ${cosa_build_date_str} +%s)"
       if [[ $(date +%s) -ge $((cosa_build_date + 60*60*24*7)) ]] ; then
         echo -e "\e[0;33m----" >&2
         echo "The COSA container image is more that a week old and likely outdated." >&2
         echo "You should pull the latest version with:" >&2
         echo "podman pull ${COREOS_ASSEMBLER_CONTAINER_LATEST}" >&2
         echo -e "----\e[0m" >&2
         sleep 10
       fi
   fi
   set -x
   podman run --rm -ti --security-opt label=disable --privileged                                    \
              --uidmap=1000:0:1 --uidmap=0:1:1000 --uidmap 1001:1001:64536                          \
              -v ${PWD}:/srv/ --device /dev/kvm --device /dev/fuse                                  \
              --tmpfs /tmp -v /var/tmp:/var/tmp --name cosa                                         \
              ${COREOS_ASSEMBLER_CONFIG_GIT:+-v $COREOS_ASSEMBLER_CONFIG_GIT:/srv/src/config/:ro}   \
              ${COREOS_ASSEMBLER_GIT:+-v $COREOS_ASSEMBLER_GIT/src/:/usr/lib/coreos-assembler/:ro}  \
              ${COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS}                                            \
              ${COREOS_ASSEMBLER_CONTAINER:-$COREOS_ASSEMBLER_CONTAINER_LATEST} "$@"
   rc=$?; set +x; return $rc
}
```
{% endraw %}

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

- `COREOS_ASSEMBLER_CONFIG_GIT`: Allows you to specifiy a local directory that
  contains the configs for the ostree you are trying to compose.
- `COREOS_ASSEMBLER_GIT`: Allows you to specify a local directory that contains
  the CoreOS Assembler scripts. This allows for quick hacking on the assembler
  itself.
- `COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS`: Allows for adding arbitrary mounts
  or args to the container runtime.
- `COREOS_ASSEMBLER_CONTAINER`: Allows for overriding the default assembler
  container which is currently
  `quay.io/coreos-assembler/coreos-assembler:latest`.

See the [Working on CoreOS Assembler](devel.md) page for examples of how
to use these variables.

### Containers and networking

You may currently need to use `--net=host` if your host is connected to a VPN
for example.

## Running persistently

At this point, try `cosa shell` to start a shell inside the container. From
here, you can run `cosa ...` to invoke build commands.

However, you can also choose to run the `cosa` bash alias above and create a
transient new container for each command. Either way, all data persists in the
build directory.

### Running multiple instances

Remove `--name cosa` from the `cosa` alias to let `podman` pick a random name
for the container and allow multiple instances of the `cosa` container to run
simultaneously. This is not the default to avoid hard to debug issues but should
be safe for running multiple instances of `kola` with different builds for
example.

## Initializing

You only need to do this once; it will clone the specified configuration repo,
and create various directories/state such as the OSTree repository. (For
production, you will want to sync this repository out so clients can get
updates).

```
$ cosa init https://github.com/coreos/fedora-coreos-config
```

The specified git repository will be cloned into `$PWD/src/config/`.

If you're doing something custom, you likely want to fork that upstream
repository.

## Performing a build

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

## Running

```
$ cosa run
```

This invokes QEMU on the image in `builds/latest`.  It uses `-snapshot`,
so any changes are thrown away after you exit qemu.  To exit, type
`Ctrl-a x`.  For more options, type `Ctrl-a ?`.

## Running with customizations

Using `coreos-assembler`, it is possible to run a CoreOS style qcow2 image
that was built elsewhere.

```
$ cosa run --qemu-image ./coreos-style.qcow2
```

Additionally, it is possible to pass in a custom Ignition config file when
running an image.  This requires the use of the `kola qemuexec` subcommand.

```
$ cosa kola qemuexec --ignition ./ignition.json --ignition-direct --qemu-image ./coreos-style.qcow2
```

There are other customizations that are posible; see the output of `cosa kola qemuexec --help`
for more options.
