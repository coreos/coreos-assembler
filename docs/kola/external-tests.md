---
parent: Testing with Kola
nav_order: 2
---

# Integrating external test suites
{: .no_toc }

1. TOC
{:toc}

## Rationale

Fedora CoreOS is comprised of a number of upstream projects, from the Linux
kernel to systemd, ostree, Ignition, podman and numerous others.

We want to support keeping tests in their respective upstream repositories, and
allow these projects to target Fedora CoreOS in e.g. their own CI. And we also
want to run unmodified upstream tests, *without rebuilding* the project.

## Using kola run with externally defined tests

The `--exttest` (`-E`) argument to `kola run` is one way to accomplish this;
you provide the path to an upstream project git repository. Tests will be found
in the `tests/kola` directory. If this project contains binaries that require
building, it is assumed that `make` (or equivalent) has already been invoked.

In addition to using `-E`, you may also copy tests to
`/usr/lib/coreos-assembler/tests/kola`.

The `tests/kola` directory will be traversed recursively to find tests.

The core idea is to express a test as a single binary (plus an optional
directory of dependencies named `data`) that will be uploaded to a CoreOS
system and run as a systemd unit, along with an optional Ignition config named
`config.ign`.

Concretely then, an external test directory can have the following content:

- `config.ign` (optional): Ignition config provided
- `config.bu` (optional): See https://coreos.github.io/butane/
- `kola.json` (optional): JSON file described below
- `data` (optional): Directory (or symlink to dir): Will be uploaded and
  available as `${KOLA_EXT_DATA}`
- one or more executables: Each executable is its own test, run independently
  with the Ignition config and/or dependency data provided.

Normally the test will be named `ext.<projname>.<subdirectory>.<executable>`.
However there is a special case to make it nicer to test Ignition configs; In
the case of a test directory with a single executable named `test.sh`, the kola
test name will be `ext.<projname>.<subdirectory>` (i.e. `test.sh` will be
omitted).

Currently the test systemd unit runs with full privileges - tests are assumed
to be (potentially) destructive and a general assumption is tests are run in
easily disposable virtual machines. A future enhancement will support
nondestructive tests.

A best practice for doing this is to write your tests in a language that
compiles to a single binary - Rust and Go for example, but there exist for
example tools like
[PyInstaller](https://realpython.com/pyinstaller-python/#pyinstaller) too.
This way you can usually avoid the need for a `data` directory.

This mechanism is suitable for testing most userspace components of CoreOS; for
example, one can have the binary drive a container runtime.

A test is considered failed if the unit exits with any non-zero exit status or
dies from any signal other than `SIGTERM`.

## Environment variables

The following environment variables are accessible to the test:
- `KOLA_EXT_DATA`: path to test data; see above
- `KOLA_UNIT`: name of systemd unit running the test itself
- `KOLA_TEST`: name of the kola test
- `KOLA_TEST_EXE`: basename of the test executable as found by kola

## Support for rebooting

An important feature of exttests is support for rebooting the host system.
This allows one to easily test OS updates for example. In order to more easily
allow sharing tests, kola has adopted a subset of the [Debian autopkgtest
interface](https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst).

See the section titled `Reboot during a test` there.  For convenience an
example is included below:


```
#!/bin/bash
# Copy of the reboot example from https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst
set -xeuo pipefail
case "${AUTOPKGTEST_REBOOT_MARK:-}" in
  "") echo "test beginning"; /tmp/autopkgtest-reboot mark1 ;;
  mark1) echo "test in mark1"; /tmp/autopkgtest-reboot mark2 ;;
  mark2) echo "test in mark2" ;;
  *) echo "unexpected mark: ${AUTOPKGTEST_REBOOT_MARK}"; exit 1;;
esac
echo "ok autopkgtest rebooting"
```

This will trigger the monitoring `kola` process to invoke a reboot.

The rationale for this is that it helps kola to know when a reboot is happening
so that it can correctly follow the state of the systemd journal, etc. A future
enhancement will support directly invoking `reboot` and having kola just figure
it out.

(Previously the API for this was to send `SIGTERM` to the current process; that
method is deprecated and will be removed at some point)

## HTTP Server

The `kolet` binary is copied into the `core` user's home directory
(`/var/home/core`) on the CoreOS system running the tests. Notably, it contains
the built-in command `kolet httpd` for starting an HTTP file server to serve the
contents of the file system.
By default, it starts the server listening on port `80` and serves the contents of
the file system at `./`; you can use the `--port` and `--path` flags to override
the defaults.

For example, if you're using a bash script as your test, you can start an HTTP
server to serve the contents at `/var/home/core` like this:
```
echo testdata > /var/home/core/testdata.txt
systemd-run /var/home/core/kolet httpd --path /var/home/core/
# It may take some time for the server to start.
sleep 1
curl localhost/testdata.txt
```

Alternatively, you can create an Ignition config (or Fedora CoreOS config) and
include it in your external test directory. This would start the HTTP server
before your test is run and may be useful if you would like to predefine the
files to serve.

In the following Fedora CoreOS config example, the Ignition config includes a
path unit and a service unit. The path unit ensures that the httpd service runs
automatically once the Kolet binary is copied to the system. Note that the path
unit has a `Before=` dependency on `kola-runext.service` to ensure that the
server is brought up before the test is run.
An HTTP server will be started at `localhost` and serve the files in `/var/www/`.
Your test can then do e.g. `curl localhost/hello_world.txt`.

Example:
```
variant: fcos
version: 1.1.0
systemd:
  units:
    - name: kolet-httpd.path
      enabled: true
      contents: |
        [Unit]
        Before=kola-runext.service
        [Path]
        PathExists=/var/home/core/kolet
        [Install]
        WantedBy=kola-runext.service
    - name: kolet-httpd.service
      contents: |
        [Service]
        ExecStart=/var/home/core/kolet httpd --path /var/www -v
        [Install]
        WantedBy=kola-runext.service
storage:
  files:
    - path: /var/www/my-kola-test-data
      contents:
        inline: Hello, world!
      mode: 0644
      user:
        name: core
      group:
        name: core
```

## `kola.json`

Kola internally supports limiting tests to specific architectures and plaforms,
as well as "clusters" of machines that have size > 1. External tests are
hardcoded to 1 machine at the moment.

Here's an example `kola.json`:

```json
{
    "architectures": "!s390x ppc64le",
    "distros": "fcos",
    "platforms": "qemu",
    "tags": "sometagname needs-internet skip-base-checks othertag",
    "requiredTag": "special",
    "additionalDisks": [ "5G" ],
    "minMemory": 4096,
    "minDisk": 15,
    "additionalNics": 2,
    "appendKernelArgs": "enforcing=0"
    "appendFirstbootKernelArgs": "ip=bond0:dhcp bond=bond0:ens5,ens6:mode=active-backup,miimon=100"
    "timeoutMin": 8,
    "exclusive": true,
    "conflicts": ["ext.config.some-test", "podman.some-other-test"],
    "description": "test description"
}
```

The only supported keys are those mentioned above; any or none may be provided
as well.  For `architectures`, `platforms` and `tags`, each value is a single
string, which is a whitespace-separated list.  The reason to use a single
string (instead of a native JSON list) is twofold.  First, it's easier to type
than a JSON list, and we don't need to support values with whitespace.  Second,
for `architectures` and `platforms` by providing `!` at the front of the
string, the value instead declares exclusions i.e. `ExclusiveArchitectures`
instead of `Architectures` in reference to kola internals.  The `distros` tag
can be used to restrict a test to run on just one distribution, either `fcos` or
`rhcos`. By default, tests will run on all distributions. (Typically, the
`distros` key is used to restrict a test to just `fcos`.)

In this example, `sometagname` and `othertag` are arbitrary tags one can use
with `kola run --tag`, but some tags have semantic meaning.

Tags with semantic meaning:

 - `needs-internet`: Taken from the Autopkgtest (linked above).  Currently only the `qemu` platform enforces this restriction.
 - `platform-independent`: This test should pass or fail on all platforms (clouds and hardware architectures); it may be run less often.
 - `skip-base-checks`: Skip built-in checks for e.g. kernel warnings on the console or systemd unit failures.

If a test has a `requiredTag`, it is run only if the required tag is specified.
In the example above, the test would only run if `--tag special` was provided.

The `additionalDisks` key has the same semantics as the `--add-disk` argument
to `qemuexec`. It is currently only supported on `qemu`.

The `injectContainer` boolean if set will cause the framework to inject
the ostree base image container into the target system; the path can be
found in the environment variable `KOLA_OSTREE_OCIARCHIVE`.  This will be
an `.ociarchive` file that can be e.g. loaded into the containers storage
via `skopeo copy oci-archive:$KOLA_OSTREE_OCIARCHIVE containers-storage:localhost/os`.

The `minDisk` key takes a size in GB and ensures that an instance type with at
least the specified amount of primary disk space is used. On QEMU, this is
equivalent to the `--qemu-size` argument to `qemuexec`. This is currently only
enforced on `qemu` and `aws`.

The `minMemory` key takes a size in MB and ensures that an instance type with
at least the specified amount of memory is used. On QEMU, this is equivalent to
the `--memory` argument to `qemuexec`. This is currently only enforced on
`qemu`.

The `additionalNics` key has the same semantics as the `--additional-nics` argument
to `qemuexec`. It is currently only supported on `qemu`.

The `appendKernelArgs` key has the same semantics at the `--kargs` argument to
`qemuexec`. It is currently only supported on `qemu`.

The `appendFirstbootKernelArgs` key has the same semantics at the `--firstbootkargs`
argument to `qemuexec`. It is currently only supported on `qemu`.

The `timeoutMin` key takes a positive integer and specifies a timeout for the test
in minutes. After the specified amount of time, the test will be interrupted.

The `exclusive` key takes a boolean value. If `true`, the test will be run by
itself in its own VM such that other tests do not conflict with it. If this key
is marked `false`, the test is run with other "non-exclusive" tests. If a test
is simple and is not expected to conflict with other tests, it should be marked
`exclusive: false`. When the `exclusive` key is not provided, tests are marked
`exclusive: true` by default.

The `conflicts` key takes a list of test names that conflict with this test.
This key can only be specified if `exclusive` is marked `false` since
`exclusive: true` tests are run exclusively in their own VM.  At runtime,
this test will be separated from the tests it is conflicting with.

More recently, you can also (useful for shell scripts) include the JSON file
inline per test, like this:

```sh
#!/bin/bash
set -xeuo pipefail
# kola: { "architectures": "x86_64", "platforms": "aws gcp", "tags": "needs-internet", "description": "test" }
test code here
```

This metadata stanza must start with `# kola: ` and have a single line of JSON.

Even more recently, you can write the test metadata as YAML inline; this is signified
by using `## kola: `.  The lines after it starting with `## ` will be parsed as metadata YAML.
For example:

```
#!/bin/bash
set -xeuo pipefail
## kola:
##   architectures: x86_64
##   platforms: "aws gcp"  # azure support is pending
##   tags: needs-internet
##   description: test description
test code here
```

A notable advantage of YAML here is support for inline comments.

## Quick Start

1. In your project's upstream repository, create the `tests/kola` directory, if
   it does not already exist
2. Move into it and find or create an appropriate subdirectory for your test to
   live in
3. Add your test to the subdirectory and make sure it is *executable* (`chmod
   a+x`)
4. Your test should now be able to be run by kola when you provide the
   `--exttest` (`-E`) argument to `kola run`

### Example

Say we want to add a simple [noop](https://en.wikipedia.org/wiki/NOP_(code))
test in the project `my-project` externally. If we follow the above
instructions, it would look like this:

```
$ git clone git@github.com:$GITHUB_USERNAME/my-project.git
$ cd my-project/tests/kola
$ $EDITOR basic/noop # Add the `noop` test
#!/bin/bash
set -xeuo pipefail
# kola: { "architectures": "x86_64", "platforms": "qemu", "tags": "needs-internet", "description": "test" }
# Test: I'm a NOOP!
test 2 -gt 1
$ chmod a+x basic/noop # Make sure the test is executable
$ cosa kola run -p qemu --qemu-image path/to/qcow2 -E path/to/my-project/ 'ext.my-project.basic' # Run the test
=== RUN   ext.my-project.basic
--- PASS: ext.my-project.basic (35.57s)
PASS, output in _kola_temp/qemu-2020-08-18-1815-2295199
```

## Fast build and iteration on your project's tests

First, use `cosa build-fast` if it applies to you (e.g. you're not working on
something in the kernel or initramfs). From your project's git repository, do
e.g.:

```
$ export COSA_DIR=/path/to/cosadir
$ cosa build-fast
$ kola run --qemu-image fastbuild*.qcow2 'ext.*'
```

Whenever you change your project's code, rerun `cosa build-fast` to create a
new qcow2. If you just changed a test script, you can just directly rerun
`kola`.

For more tips, see also the [Working with CoreOS Assembler](../working.md).
