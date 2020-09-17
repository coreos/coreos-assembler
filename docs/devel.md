---
layout: default
nav_order: 7
---

# Working on CoreOS Assembler
{: .no_toc }

This page is about CoreOS Assembler development. See the [Building Fedora
CoreOS](building-fcos.md) and [Working with CoreOS Assembler](working.md)
guides if you are looking for how to use the CoreOS Assembler.

1. TOC
{:toc}

## Hacking on CoreOS Assembler Scripts

If you find yourself wanting to hack on CoreOS Assembler itself then you can
easily mount the scripts into the container and prevent rebuilding the
container to test every change. This can be done using the
`COREOS_ASSEMBLER_GIT` env var.

```
$ export COREOS_ASSEMBLER_GIT=/path/to/github.com/coreos/coreos-assembler/
$ cosa init https://github.com/coreos/fedora-coreos-config.git
$ cosa fetch && cosa build
```

## Installing cosa inside an existing container

If you already have a pet container you'd like to keep using that matches the
same Fedora release cosa uses, you can install cosa inside of it by doing:

```
$ sudo ./build.sh configure_yum_repos
$ sudo ./build.sh install_rpms
$ make
$ sudo make install
```

From that point on, you only need to run `make && sudo make install` if you're
hacking on cosa itself (unless there are new RPM requirements added).

You can also reuse a `cosa shell` to test changes from other git repositories.
You'll likely want to do this for e.g. things like testing out changes to
`ostree`/`rpm-ostree` that are run as part of `cosa build`.

## Building the cosa container image locally

To completely rebuild the COSA container image locally, use:

```
$ podman build -t localhost/coreos-assembler .
```

You should then set `COREOS_ASSEMBLER_CONTAINER=localhost/coreos-assembler` in
the environment if you're using the bash alias `cosa`:

```
$ export COREOS_ASSEMBLER_CONTAINER=localhost/coreos-assembler
$ cosa ...
```

You can also use `Dockerfile.dev` to create a new container image based on the
official one with your local cahnges:

```
$ podman build -f Dockerfile.dev -t localhost/coreos-assembler
```

## Developing on coreos-assembler remotely

Many coreos-assembler developers use `podman` locally.  However some things
may only reproduce in a Kubernetes/OpenShift environment.  One trick is to
spin up a pod with coreos-assembler with an entrypoint of `sleep infinity`,
then use `oc rsh` to log into it.

A further trick you can use is `oc rsync` to copy the build from your
workstation to the remote pod for fast iteration.  For example, assuming
a remote pod name of `walters-cosa`:

`oc rsync ./ walters-cosa:/home/builder/coreos-assembler/ && oc rsh walters-cosa sudo /bin/sh -c 'cd ~builder/coreos-assembler && make install'`

(This could be improved in various ways, among them just shipping the binaries and not the source)

## Pulling in fixed packages into the COSA container

To pull in fixed packages before they make it through Bodhi,
you can simply tag them into the
`f${releasever}-coreos-continuous` tag and trigger a
rebuild.

## Running Unit Tests

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

## Adding/Updating kola Tests in coreos-assembler

For adding tests to `kola`, please see the [Testing with Kola](kola.md) page.

You can then run `make` to build your modifications.
