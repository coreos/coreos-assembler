---
has_children: true
nav_order: 8
---

# Testing with Kola
{: .no_toc }

Kola is a framework for testing software integration in CoreOS systems
across multiple platforms. It is primarily designed to operate within
the CoreOS Assembler for testing software that has landed in the OS image.

Kola supports running tests on multiple platforms, currently QEMU, GCP,
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

By default, kola uses the `qemu` platform with the most recently built image
(assuming it is run from within a CoreOS Assembler working directory).

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

It's also possible to skip tests based on tags by prefixing
the tag by `!`:

`kola run --tag '!reprovision'`

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

The special pattern `skip-console-warnings` suppresses the default check for kernel errors on the console which would otherwise fail a test.

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

## kola non-exclusive tests

Some tests are light weight and do not involve complex interactions like reboots
and multiple machines. Tests that are not expected to conflict with other tests can be
marked as "non-exclusive", so that they are run in the same VM to save resources.

External tests can be marked as non-exclusive via kola.json or an inline tag. 
Note: tests compiled in kola (non external tests) cannot be marked as non-exclusive. 
This is deliberate as tests compiled in kola should be complex and thus exclusive.

## Manhole

The `platform.Manhole()` function creates an interactive SSH session which can
be used to inspect a machine during a test.

The `--ssh-on-test-failure` flag can be specified to have the kola runner
automatically SSH into a machine when any `MustSSH` calls fail.

## kolet

kolet is run on kola instances to run native functions in tests. Generally kolet
is not invoked manually.

## More information on tests

After you run the kola test, you can find more information in `tmp/kola/<test-name>` about the test that just ran, as the following file logs. They will help you to debug the problem and will certainly give you hints along the way.

1. `journal.txt`
2. `console.txt`
3. `ignition.json`
4. `journal-raw.txt.gz`

## Extended artifacts

1. Extended artifacts need additional forms of testing (You can pass the ignition and the path to the artifact you want to test)
2. `cosa kola run -h` (this allows you to see the commands yourself and what syntax is needed)
3. `cosa buildextend-"name_of_artifact"` (An example of building an extended artifact)
4. `kola run -p <platform>` Is the most generic way of testing extended artifacts, this is mostly useful for the cloud platforms
5. For running the likes of metal/metal4k artifacts there's not much difference than running `kola run` from the coreos-assembler
6. `cd builds/latest/` (This will show your latest build information)
7. `cosa list` (This will show you the most recent CoreOS builds that have been made and the artifacts that were created)
8. In the case of the `testiso` command, you can determine what tests are running by looking for the pattern in the test name. It will follow: `test-to-run.disk-type.networking.multipath.firmware`. For example, the `iso-live-login.4k.uefi`, attempts to install FCOS/RHCOS to a disk that uses 4k sector size. If you don't see the 4k pattern, the `testiso` command will attempt to install FCOS/RHCOS to a non 4k disk (512b sector size).
9. `cosa kola testiso iso-offline-install.mpath.uefi` (This is an example testing the live ISO build with no internet access using multipath and the uefi firmware.)

Example output:

```
kola -p qemu testiso --inst-insecure --output-dir tmp/kola
Ignoring verification of signature on metal image
Running test: iso-as-disk.bios
PASS: iso-as-disk.bios (12.408s)
Running test: iso-as-disk.uefi
PASS: iso-as-disk.uefi (16.039s)
Running test: iso-as-disk.uefi-secure
PASS: iso-as-disk.uefi-secure (16.994s)
```

## Useful commands

`cosa kola run 'name_of_test'` This is how to run a single test, This is used to help debug specific tests in order to get a better understanding of the bug that's taking place. Once you run this command this test will be added to the tmp directory

`cosa kola run basic` This will just run the basic tests

`cosa kola run --parallel=3` This will run tests in parallel, 3 at a time.

In order to see the logs for these tests you must enter the `tmp/kola/name_of_the_tests` and there you will find the logs (journal and console files, ignition used and so on)

`cosa run` This launches the build you created (in this way you can access the image for troubleshooting). Also check the option -c (console).

`cosa run -i ignition_path` You can run it passing your Ignition, or the Ignition used in the the test that failed for troubleshooting reasons.

`kola list --json | jq -r '.[] | [.Name,.Description]| @tsv'` This will list all tests name and the description.

## Run tests on cloud platforms
`cosa kola run -p aws --aws-ami ami-0431766f2498820b8 --aws-region us-east-1 basic` This will run the basic tests on AWS using `ami-0431766f2498820b8` (fedora-coreos-37.20230227.20.2) with default instance type `m5.large`. Add `--aws-type <t3.micro>` if you want to use custom type. How to create the credentials refer to https://github.com/coreos/coreos-assembler/blob/main/docs/mantle/credentials.md#aws

`kola run -p=gcp --gcp-image=projects/fedora-coreos-cloud/global/images/fedora-coreos-37-20230227-20-2-gcp-x86-64 --gcp-json-key=/data/gcp.json --gcp-project=fedora-coreos-testing basic` This will run the basic tests on GCP using default machine type `n1-standard-1`.
- `gcp-image` is in the format of `projects/<GCP Image Project>/global/images/<GCP Image Name>`, to find related info refer to https://builds.coreos.fedoraproject.org/browser?stream=testing-devel&arch=x86_64.
- `gcp-json-key` is using a service account's JSON key for authentication, how to create service account keys refer to https://github.com/coreos/coreos-assembler/blob/main/docs/mantle/credentials.md#gcp.
- `gcp-project` is meant for testing in the specified project, or it will use the same as `<GCP Image Project>`.