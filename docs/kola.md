---
has_children: true
nav_order: 8
---

# Testing with Kola
{: .no_toc }

Kola is a framework for testing software integration in CoreOS systems
across multiple platforms. It is primarily designed to operate within
the CoreOS Assembler for testing software that has landed in the OS image.

Kola supports running tests on multiple platforms, currently QEMU, GCE,
AWS, VMware VSphere, Packet, and OpenStack. In the future systemd-nspawn and
other platforms may be added.
Local platforms do not rely on access to the Internet as a design
principle of kola, minimizing external dependencies. Any network
services required get built directly into kola itself. Machines on cloud
platforms do not have direct access to the kola so tests may depend on
Internet services such as discovery.etcd.io or quay.io instead.

Kola outputs assorted logs and test data to `_kola_temp` for later
inspection.

Kola is still under heavy development and it is expected that its
interface will continue to change.

By default, kola uses the `qemu-unprivileged` platform with the most recently
built image (assuming it is run from within a CoreOS Assembler working
directory).

1. TOC
{:toc}

## kola run

The run command invokes the main kola test harness. It
runs any tests whose registered names matches a glob pattern.

`kola run <glob pattern>`

`--denylist-test` can be used if one or more tests in the pattern should be skipped.
This switch may be provided once:

`kola --denylist-test linux.nfs.v3 run`

multiple times:

`kola --denylist-test linux.nfs.v3 --denylist-test linux.nfs.v4 run`

and can also be used with glob patterns:

`kola --denylist-test linux.nfs* --denylist-test crio.* run`

Tests specified in `src/config/kola-denylist.yaml` will also be skipped
regardless of whether the switch `--denylist-test` was provided.

Example format of the file:

```yaml
- pattern: test1.blobpattern.*
  tracker: https://github.com/coreos/coreos-assembler/pull/123
  streams:
    # This test will be skipped in these streams
    # If no streams are specified, test will be skipped on all streams
    - stream1
    - stream2
  # The test will only be skipped until this date (will resume on the date)
  # Format: YYYY-MM-DD
  snooze: 2021-07-20
  arches:
    # This test will be skipped on these arches
    # If no arches are specified, test will be skipped on all arches
    - s390x
  platforms:
    # This test will be skipped on these platforms
    # If no platforms are specified, test will be skipped on all platforms
    - openstack
    - aws
- pattern: test2.test
  ...
```
## kola list

The list command lists all of the available tests.

## kola spawn

The spawn command launches CoreOS instances.

## kola bootchart

The bootchart command launches an instance then generates an svg of the boot
process using `systemd-analyze`.

## kola subtest parallelization

Subtests can be parallelized by adding `c.H.Parallel()` at the top of the
inline function given to `c.Run`. It is not recommended to utilize the
`FailFast` flag in tests that utilize this functionality as it can have
unintended results.

## kola test namespacing

The top-level namespace of tests should fit into one of the following categories:

1. Groups of tests targeting specific packages/binaries may use that namespace
   (ex: `docker.*`)
2. Tests that target multiple supported distributions may use the `coreos`
   namespace.
3. Tests that target singular distributions may use the distribution's
   namespace.

## kola test registration

Registering kola tests currently requires that the tests are registered
under the kola package and that the test function itself lives within
the mantle codebase.

Groups of similar tests are registered in an init() function inside the kola
package. `Register(*Test)` is called per test. A kola `Test` struct requires a
unique name, and a single function that is the entry point into the test.
Additionally, userdata (such as an Ignition config) can be supplied. See the
`Test` struct in
[kola/register/register.go](https://github.com/coreos/coreos-assembler/blob/main/mantle/kola/register/register.go)
for a complete list of options.

## kola test writing

A kola test is a go function that is passed a `platform.TestCluster` to
run code against.  Its signature is `func(platform.TestCluster)`
and must be registered and built into the kola binary.

A `TestCluster` implements the `platform.Cluster` interface and will give you
access to a running cluster of CoreOS machines. A test writer can interact with
these machines through this interface.

To see test examples look under
[kola/tests](https://github.com/coreos/coreos-assembler/blob/main/mantle/kola/tests)
in the mantle codebase.

For a quickstart see [kola/adding-tests.md](kola/adding-tests.md).

## kola native code

For some tests, the `Cluster` interface is limited and it is desirable to run
native go code directly on one of the CoreOS machines. This is currently
possible by using the `NativeFuncs` field of a kola `Test` struct. This like a
limited RPC interface.

`NativeFuncs` is used similar to the `Run` field of a registered kola test. It
registers and names functions in nearby packages.  These functions, unlike the
`Run` entry point, must be manually invoked inside a kola test using a
`TestCluster`'s `RunNative` method. The function itself is then run natively on
the specified running CoreOS instances.

For more examples, look at the
[coretest](https://github.com/coreos/coreos-assembler/tree/main/mantle/kola/tests/coretest)
suite of tests under kola. These tests were ported into kola and make
heavy use of the native code interface.

## Manhole

The `platform.Manhole()` function creates an interactive SSH session which can
be used to inspect a machine during a test.

The `--ssh-on-test-failure` flag can be specified to have the kola runner
automatically SSH into a machine when any `MustSSH` calls fail.

## kolet

kolet is run on kola instances to run native functions in tests. Generally kolet
is not invoked manually.
