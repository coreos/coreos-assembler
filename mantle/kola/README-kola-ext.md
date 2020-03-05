Integrating external test suites
===

Fedora CoreOS is comprised of a number of upstream projects, from
the Linux kernel to systemd, ostree, Ignition, podman and
numerous others.

We want to support keeping tests in their respective upstream
repositories, and allow these projects to target Fedora CoreOS
in e.g. their own CI.  And we also want to run unmodified
upstream tests, *without rebuilding* the project.

Using kola run-ext-bin
===

The `kola run-ext-bin` is one way to accomplish this.  The
core idea is to express your test suite as a binary (plus an optional
directory of dependencies) that will be uploaded to a CoreOS
system and run as a systemd unit.

Currently this systemd unit runs with full privileges - tests
are assumed to be (potentially) destructive and a general assumption
is tests are run in easily disposable virtual machines.

A best practice for doing this is to write your tests in a language
that compiles to a single binary - Rust and Go for example, but
there exist for example tools like [PyInstaller](https://realpython.com/pyinstaller-python/#pyinstaller)
too.

This mechanism is suitable for testing most userspace components
of CoreOS; for example, one can have the binary drive a container runtime.

An important feature of `run-ext-bin` is support for rebooting the host system.
This allows one to easily test OS updates.  To do this, simply invoke the usual
`reboot` - the test framework will monitor the target systemd unit
and ignore the case where it exits with `SIGTERM`.

A test is considered failed if the unit exits with any non-zero exit
status or dies from any signal other than `SIGTERM`.

