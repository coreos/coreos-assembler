# Adding/Updating kola Tests in coreos-assembler

For adding tests to `kola`, please see the [kola test quickstart](https://github.com/coreos/coreos-assembler/blob/master/mantle/kola/README.md).

You can then run `make` to build your modifications.

# Running Unit Tests

1. Ensure that `pytest` and `pytest-cov` are installed:

```
$ pip3 install --user -r test-requirements.txt
```

2. Run `pytest` on the `tests` directory

```
$ pytest tests/
============================= test session starts ==============================
platform linux -- Python 3.7.3, pytest-4.6.3, py-1.8.0, pluggy-0.12.0
rootdir: /var/home/steve/Tech/GITHUB/coreos-assembler, inifile: pytest.ini
plugins: cov-2.7.1
collected 3 items

tests/test_cli.py ...                                                    [100%]

----------- coverage: platform linux, python 3.7.3-final-0 -----------
Name                      Stmts   Miss  Cover
---------------------------------------------
src/cosalib/__init__.py       0      0   100%
src/cosalib/build.py        127    127     0%
src/cosalib/cli.py           28      0   100%
---------------------------------------------
TOTAL                       155    127    18%


=========================== 3 passed in 0.05 seconds ===========================
```

# Using overrides

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

`$ make install DESTDIR=/path/to/cosa-workdir/overrides/rootfs`

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

# Using cosa run --srv for even faster iteration

If you're working on e.g. the kernel or Ignition (things that go into the initramfs),
then you probably need a `cosa build` workflow.  However, let's say you want to
test a change to something much later in the boot process - e.g. `podman`.  Rather
than doing a full image build each time, a fast way to test out changes is to use
something like this:

```
$ cosa run --srv ~/src/github/containers/libpod
```

Then in the booted VM, `/srv` will point to the `libpod` directory on your host,
allowing you to directly execute binaries from there.  You can also use e.g.
`rpm-ostree usroverlay` and then copy binaries from your host `/srv` into
the VM's rootfs.

# Developing on coreos-assembler remotely

Many coreos-assembler developers use `podman` locally.  However some things
may only reproduce in a Kubernetes/OpenShift environment.  One trick is to
spin up a pod with coreos-assembler with an entrypoint of `sleep infinity`,
then use `oc rsh` to log into it.

A further trick you can use is `oc rsync` to copy the build from your
workstation to the remote pod for fast iteration.  For example, assuming
a remote pod name of `walters-cosa`:

`oc rsync ./ walters-cosa:/home/builder/coreos-assembler/ && oc rsh walters-cosa sudo /bin/sh -c 'cd ~builder/coreos-assembler && make install'`

(This could be improved in various ways, among them just shipping the binaries and not the source)
