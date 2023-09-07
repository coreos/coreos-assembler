---
nav_order: 4
---

# Working with CoreOS Assembler
{: .no_toc }

1. TOC
{:toc}

## Understanding "config git"

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

## Components of "config git"

### `manifest.yaml`

For generating OSTree commits, cosa uses `manifest.yaml`: An rpm-ostree
"manifest" or "treefile", which mostly boils down to a list of RPMs and a set
of rpm-md repositories they come from. It also supports `postprocess` to make
arbitrary changes. See the rpm-ostree documentation for the [Treefile format
reference](https://coreos.github.io/rpm-ostree/treefile/).

### `overlay.d/`

coreos-assembler also supports a generic way to embed architecture-independent
configuration and scripts by creating subdirectories in `overlay.d/`.  Each subdirectory
of the `overlay.d.` directory is added to the OSTree commit, in lexicographic order.
It's recommended to name directories with a numeric prefix - e.g. `05core`, `10extras`.
Non-directories are ignored.  For example, a good practice is to add a `README.md` file
into your overlay directories describing their structure.

### `image.yaml`

This YAML file configures the output disk images.  Supported keys are:

- `size`: Size in GB for cloud images (OpenStack, AWS, etc.)  Required.
- `extra-kargs`: List of kernel arguments.
- `include`: path to another YAML file to include. List values are appended. For other type,
  the values in the sourcing config win.

It's likely in the future we will extend this to support e.g. a separate `/var`
partition or configuring the filesystem types.  If you want to do anything like
that today it requires forking the assembler and rebuilding it.
See the fedora-coreos-config for an example.

## Hacking on "config git"

First you can expand the size of the image; edit `src/config/image.yaml` and
e.g. change `8` to `9`. Rerun `cosa build`, and notice that the OSTree commit
didn't change, but a new image is generated in `builds`. When you `cosa run`,
you'll get it.

Another thing to try is editing `src/config/manifest.yaml` - add or
remove entries from `packages`.  You can also add local rpm-md `file:///`
repositories.


## Using overrides

Development speed is closely tied to the "edit-compile-debug" cycle.  coreos-assembler
supports an `overrides/` sub-directory of the coreos-assembler working directory,
which allows easily overlaying locally-generated content on top of the base OS content.

There are two subdirectories of `overrides/`:

- `overrides/rootfs`
- `overrides/rpm`

Let's say you want to hack on both ostree and ignition-dracut.  See
for example [this PR](https://github.com/coreos/ignition-dracut/pull/106)
which added support for `make install DESTDIR=` to the latter.  In general
most upstream build systems support something like this; if they don't
it's a good idea to add.

Concretely, if `/path/to/cosa-workdir` is where you ran `cosa init`,
then after doing edits in a project, run a command like this from
the source repository for the component:

```
$ make install DESTDIR=/path/to/cosa-workdir/overrides/rootfs
```

This would then install files like
`/path/to/cosa-buildroot/overrides/rootfs/usr/bin/ostree`
etc.

If you then run `cosa build` from the cosa workdir,
those overrides will be automatically incorporated.

You can also choose to use `overrides/rpm` - this accepts pre-built
binary RPMs.  This can be convenient when you want to quickly test
a binary RPM built elsewhere, or if you want to go through a more
"official" build process.  If any RPMs are present here, then coreos-assembler
will automatically run `createrepo_c` and ensure that they are used
in the build.

In the future, it's likely coreos-assembler will also support something
like `overrides/src` which could be a directory of symlinks to local
git repositories.

## Using cosa build-fast

If you're working on e.g. the kernel or Ignition (things that go into the initramfs),
then you probably need a `cosa build` workflow (or `cosa buildinitramfs-fast`, see below).
However, let's say you want to test a change to something that runs purely in the real root,
such as e.g. `rpm-ostree`, `podman`, or `console-login-helper-messages`.  Rather
than doing a full image build each time, a fast way to test out changes is to use
`cosa build-fast`.

This command assumes you have a previous local coreos-assembler build.  From
the git checkout of the project you want to add:

```
$ export COSA_DIR=/srv/builds/fcos
$ cosa build-fast
$ cosa run
```

The `cosa build-fast` command will run `make` and inject the resulting binaries
on a qcow2 overlay file, which will appear in your project working directory.
The `cosa run` command similarly knows to look for these `qcow2` files.

This will not affect the "real" cosa build in `/srv/builds/fcos`, but will
use it as a data source.

## Using cosa run --bind-ro for even faster iteration

This workflow can be used alongside `cosa build-fast`, or separate from it.
If you invoke e.g.

```
$ cosa run --bind-ro ~/src/github/containers/podman,/run/workdir
```

The target VM will have the source directory bound in `/run/workdir`; then you
can directly copy binaries from your development environment into the VM.

If you are running cosa in a container, you will have to change your current
working directory to a parent directory common to both project directories and
use relative paths:

```
$ cd ~
$ cosa run \
      --qemu-image src/fcos/build/latest/x86_64/fedora-coreos-*.x86_64.qcow2 \
      --bind-ro src/github/containers/podman,/run/workdir
```

Then in the booted VM, `/run/workdir` will point to the `libpod` directory on your host,
allowing you to directly execute binaries from there.  You can also use e.g.
`rpm-ostree usroverlay` and then copy binaries from your host `/run/workdir` into
the VM's rootfs.

## Using host binaries

Another related trick is:

```
$ cosa run --bind-ro /usr/bin,/run/hostbin
```

Then in the VM you have e.g. `/run/hostbin/strace`.  (This may fail in some scenarios
where your dev container is different than the target).

If you are running cosa in a container, you will only have access to the binary
installed in this container. You can install binaries before launching the VM
with:

```
$ cosa shell
$ sudo dnf install ...
$ cosa run --bind-ro /usr/bin,/run/hostbin
```

## Using cosa buildinitramfs-fast

If you're iterating on changes *just* to the initramfs, you can also use
`cosa buildinitramfs-fast`.  For example, suppose you are working on `ignition`.
Follow these steps:

```
$ make
$ install -D -m 0755 bin/amd64/ignition /path/to/cosadir/overrides/initramfs/usr/bin/ignition
$ cd /path/to/cosadir
$ cosa buildinitramfs-fast
$ cosa run --qemu-image tmp/fastbuild/fastbuildinitrd-fedora-coreos-qemu.qcow2 -i config.ign
```

(Or instead of `cosa run` use e.g. `cosa kola` to run tests, etc.)

If you're adding new fields to the Ignition experimental spec, `-i` will
silently remove those fields, since the copy of Ignition that's vendored
into mantle won't know about them yet.  Instead, you can specify that
kola should pass the config to the machine unmodified:

```
$ kola qemuexec --qemu-image tmp/fastbuild/fastbuildinitrd-fedora-coreos-qemu.qcow2 -i config.ign --ignition-direct
```

You'll need to
[manually configure autologin](https://docs.fedoraproject.org/en-US/fedora-coreos/tutorial-autologin/)
in the Ignition config, since kola won't be able to do it for you.

## Performing an in-place OS update manually

The output of coreos-assembler is conceptually two things:

- an ostree container image
- disk images (ISO, AWS AMI, qemu .qcow2, etc)

In many cases, rather than booting from a new disk image with the new OS, you will
want to explicitly test in-place upgrades.  This uses an [ostree native container](https://fedoraproject.org/wiki/Changes/OstreeNativeContainer), which is in the form of an `.ociarchive` file generated
by `cosa build container` (as well as the default `cosa build`, which *also* generates a `qemu` disk image).

You will need to make the container image available to your targeted system (VM or physical).  One
way to do this is to push the container to a public registry such as quay.io:

`cosa push-container quay.io/exampleuser/fcos`

Performing an in-place update from the `cosa build` output boils down to invoking a command of the form:

```
$ rpm-ostree rebase --experimental ostree-unverified-registry:quay.io/exampleuser/fcos
$ systemctl reboot
```

## Using different CA certificates

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


## Running CoreOS Assembler in OpenShift on Google Compute Platform

This is a guide to run a COSA pod in an OpenShift 4.6+ cluster in Google
Compute Platform (GCP).

[Install the KVM device plugin](https://github.com/cgwalters/kvm-device-plugin).

Up to this point, you needed to be `kubeadmin`.
From this point on though, best practice is to switch to an "unprivileged" user.

(In fact the steps until this point could be run by a separate team
 that manages the cluster; other developers could just use it as unprivileged users)

Personally, I added a [httpasswd identity provider](https://docs.openshift.com/container-platform/4.1/authentication/identity_providers/configuring-htpasswd-identity-provider.html)
and logged in with a password.

I also did `oc new-project coreos-virt` etc.

Schedule a cosa pod:

```
apiVersion: v1
kind: Pod
metadata:
  labels:
    run: cosa
  name: cosa
spec:
  containers:
  - args:
    - shell
    - sleep
    - infinity
    image: quay.io/coreos-assembler/coreos-assembler:latest
    name: cosa
    resources:
      requests:
        # Today COSA hardcodes 2048 for launching VMs.  We could
        # probably shrink that in the future.
        memory: "3Gi"
        devices.kubevirt.io/kvm: "1"
      limits:
        memory: "3Gi"
        devices.kubevirt.io/kvm: "1"
    volumeMounts:
    - mountPath: /srv
      name: workdir
  volumes:
  - name: workdir
    emptyDir: {}
  restartPolicy: Never
```

Then `oc rsh pods/cosa` and you should be able to `ls -al /dev/kvm` - and `cosa build` etc!
