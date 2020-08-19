# Integrating external test suites

## Rationale

Fedora CoreOS is comprised of a number of upstream projects, from
the Linux kernel to systemd, ostree, Ignition, podman and
numerous others.

We want to support keeping tests in their respective upstream
repositories, and allow these projects to target Fedora CoreOS
in e.g. their own CI.  And we also want to run unmodified
upstream tests, *without rebuilding* the project.

## Using kola run with externally defined tests

The `--exttest` (`-E`) argument to `kola run` is one way to accomplish this; you
provide the path to an upstream project git repository.  Tests will be found
in the `tests/kola` directory.  If this project contains binaries that require
building, it is assumed that `make` (or equivalent) has already been invoked.

In addition to using `-E`, you may also copy tests to `/usr/lib/coreos-assembler/tests/kola`.

The `tests/kola` directory will be traversed recursively to find tests.

The core idea is to express a test as a single binary (plus an optional
directory of dependencies named `data`) that will be uploaded to a CoreOS
system and run as a systemd unit, along with an optional Ignition config
named `config.ign`.

Concretely then, an external test directory can have the following content:

- `config.ign` (optional): Ignition config provided
- `kola.json` (optional): JSON file described below
- `data` (optional): Directory (or symlink to dir): Will be uploaded and available as `${KOLA_EXT_DATA}`
- one or more executables: Each executable is its own test, run independently
  with the Ignition config and/or dependency data provided.

In the case of a test directory with a single executable, the kola test name will be
`ext.<projname>.<subdirectory>`.  Otherwise, the test will be named `ext.<projname>.<subdirectory>.<executable>`.

Currently the test systemd unit runs with full privileges - tests
are assumed to be (potentially) destructive and a general assumption
is tests are run in easily disposable virtual machines.  A future
enhancement will support nondestructive tests.

A best practice for doing this is to write your tests in a language
that compiles to a single binary - Rust and Go for example, but
there exist for example tools like [PyInstaller](https://realpython.com/pyinstaller-python/#pyinstaller)
too.  This way you can usually avoid the need for a `data` directory.

This mechanism is suitable for testing most userspace components
of CoreOS; for example, one can have the binary drive a container runtime.

A test is considered failed if the unit exits with any non-zero exit
status or dies from any signal other than `SIGTERM`.

## Support for rebooting

An important feature of exttests is support for rebooting the host system.
This allows one to easily test OS updates for example.  In order to
more easily allow sharing tests, kola has adopted a subset of the
[Debian autopkgtest interface](https://salsa.debian.org/ci-team/autopkgtest/raw/master/doc/README.package-tests.rst).

See the section titled `Reboot during a test` there.  For convenience
an example is included below:


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

The rationale for this is that it helps kola to know when a reboot
is happening so that it can correctly follow the state of the systemd
journal, etc.  A future enhancement will support directly invoking
`reboot` and having kola just figure it out.

(Previously the API for this was to send `SIGTERM` to the current process;
 that method is deprecated and will be removed at some point)

## `kola.json`

Kola internally supports limiting tests to specific architectures and plaforms,
as well as "clusters" of machines that have size > 1.  External tests
are hardcoded to 1 machine at the moment.

Here's an example `kola.json`:

```json
{
    "architectures": "!s390x ppc64le",
    "platforms": "qemu-unpriv",
    "tags": "sometagname needs-internet othertag",
    "additionalDisks": [ "5G" ],
    "minMemory": 4096
}
```

The only supported keys are those mentioned above; any or none
may be provided as well.  For `architectures`, `platforms` and `tags`, 
each value is a single string, which is a whitespace-separated list.
The reason to use a single string (instead of a native JSON list)
is twofold.  First, it's easier to type than a JSON list, and
we don't need to support values with whitespace.
Second, for `architectures` and `platforms` by providing `!` at
the front of the string, the value instead declares exclusions
i.e. `ExclusiveArchitectures` instead  of `Architectures` in
reference to kola internals.

In this example, `sometagname` and `othertag` are arbitrary tags
one can use with `kola run --tags`, but the `needs-internet` tag
has semantic meaning, also taken from the Autopkgtest (linked above).
Currently only the `qemu` platform enforces this restriction.

The `additionalDisks` key has the same semantics as the `--add-disk`
argument to `qemuexec`. It is currently only supported on `qemu-unpriv`.

The `minMemory` key takes a size in MB and ensures that an instance type
with at least the specified amount of memory is used. On QEMU, this is
equivalent to the `--memory` argument to `qemuexec`. This is currently
only enforced on `qemu-unpriv`.

More recently, you can also (useful for shell scripts) include the JSON file
inline per test, like this:

```sh
#!/bin/bash
set -xeuo pipefail
# kola: { "architectures": "x86_64", "platforms": "aws gcp", "tags": "needs-internet" }
test code here
```

This metadata stanza must start with `# kola: ` and have a single line of JSON.

## Quick Start
1. In your project's upstream repository, create the `tests/kola` directory, if 
   it does not already exist
2. Move into it and find or create an appropriate subdirectory for your test to 
   live in
3. Add your test to the subdirectory and make sure it is *executable* (`chmod a+x`)
4. Your test should now be able to be run by kola when you provide the `--exttest`
   (`-E`) argument to `kola run`

### Example
Say we want to add a simple [noop](https://en.wikipedia.org/wiki/NOP_(code)) 
test in the project `my-project` externally. If we follow the above instructions, 
it would look like this:
```
$ git clone git@github.com:$GITHUB_USERNAME/my-project.git
$ cd my-project/tests/kola
$ $EDITOR basic/noop # Add the `noop` test
#!/bin/bash
set -xeuo pipefail
# kola: { "architectures": "x86_64", "platforms": "qemu", "tags": "needs-internet" }
# Test: I'm a NOOP!
test 2 -gt 1
$ chmod a+x basic/noop # Make sure the test is executable
$ cosa kola run -p qemu --qemu-image path/to/qcow2 -E path/to/my-project/ 'ext.my-project.basic' # Run the test
=== RUN   ext.my-project.basic
--- PASS: ext.my-project.basic (35.57s)
PASS, output in _kola_temp/qemu-unpriv-2020-08-18-1815-2295199
```