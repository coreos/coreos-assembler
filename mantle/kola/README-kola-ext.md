Integrating external test suites
=

Rationale
---

Fedora CoreOS is comprised of a number of upstream projects, from
the Linux kernel to systemd, ostree, Ignition, podman and
numerous others.

We want to support keeping tests in their respective upstream
repositories, and allow these projects to target Fedora CoreOS
in e.g. their own CI.  And we also want to run unmodified
upstream tests, *without rebuilding* the project.

Using kola run with externally defined tests
---

The `--exttest` (`-E`) argument to `kola run` one way to accomplish this; you
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

Support for rebooting
---

An important feature of exttests is support for rebooting the host system.
This allows one to easily test OS updates for example.  To do this, your
test process should send `SIGTERM` to itself.  For example, in bash use:

`kill -TERM $$`

This will trigger the monitoring `kola` process to invoke a reboot.

The rationale for this is that it helps kola to know when a reboot
is happening so that it can correctly follow the state of the systemd
journal, etc.  A future enhancement will support directly invoking
`reboot` and having kola just figure it out.

`kola.json`
---

Kola internally supports limiting tests to specific architectures and plaforms,
as well as "clusters" of machines that have size > 1.  External tests
are hardcoded to 1 machine at the moment.

Here's an example `kola.json`:

```json
{
    "architectures": "!s390x ppc64le",
    "platforms": "qemu-unpriv"
}
```

The only supported keys are those two; either or none may be provided as well.
Each value is a single string, which is a whitespace-separated list.
The reason to use a single string (instead of a native JSON list)
is that by providing `!` at the front of the string, the value instead
declares exclusions (`ExclusiveArchitectures` instead of `Architectures` in
reference to kola internals.

More recently, you can also (useful for shell scripts) include the JSON file
inline per test, like this:

```sh
#!/bin/bash
set -xeuo pipefail
# kola: { "architectures": "x86_64", "platforms": ["aws", "gcp"] }
test code here
```

This metadata stanza must start with `# kola: ` and have a single line of JSON.
