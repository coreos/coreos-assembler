---
parent: Testing with Kola
nav_order: 1
---

# Adding Tests to Kola
{: .no_toc }

1. TOC
{:toc}

## Quick Start

1. Fork and clone the [`coreos-assembler` repository](https://github.com/coreos/coreos-aasembler/)
2. Move into `mantle/kola/tests/` and look for the package your test would best fit
3. Edit the file and add your test(s), ensuring that you register your new test(s) in the packages `init()`
4. Commit, push, and PR your result

## Relationship to external tests

If your test can be run as an [external test](external-tests.md) (i.e. from a
script which runs directly on the host), then that approach is preferred. Most
likely, the test would live in the fedora-coreos-config repo (or if tightly
coupled to a specific project, in that project's repo; there are plans to make
those more consumable by FCOS/RHCOS).

If your test needs richer information about e.g. the cosa build, or multiple
nodes, or more support code from outside the test subject, then adding a
native kola test as described here is a better fit.

### Example

Say we wanted to add a simple [noop](https://en.wikipedia.org/wiki/NOP_(code)) test in the `podman` test package. If we follow the above instructions it would look like this:

```
$ git clone git@github.com:$GITHUB_USERNAME/coreos-assembler.git
<snip/>
$ cd mantle
$ pushd kola/tests/
$ $EDITOR podman/podman.go  # Add the test
// Test: I'm a NOOP!
func podmanNOOP(c cluster.TestCluster) {
    // NOOP!
}
$ $EDITOR podman/podman.go # Register the test in the init
func init() {
    register.RegisterTest(&register.Test{
        Run:         podmanNOOP,
        ClusterSize: 1,
        Name:        `podman.noop`,
        Distros:     []string{"rhcos"},
    })
<snip/>
$ popd
$ ./build kola
# Check and ensure the test is there
$ ./bin/kola list | grep podman
podman.base                                     [all]                                   [all]   [rhcos]
podman.network                                  [all]                                   [all]   [rhcos]
podman.noop                                     [all]                                   [all]   [rhcos]
podman.workflow                                 [all]                                   [all]   [rhcos]
# Run your test and see what happens
$ ./bin/kola run -b rhcos --qemu-image rhcos-410.8.20190502.0-qemu.qcow2 podman.noop
=== RUN   podman.noop
--- PASS: podman.noop (21.08s)
PASS, output in _kola_temp/qemu-2019-05-08-1535-16606
# git add/commit/push...
# Open PR to get the test added!
```

## Grouping Tests

Sometimes it makes sense to group tests together under a specific package, especially when these tests are related and require the same test parameters. For `kola` it only takes a forwarding function to do testing groups. This forwarding function should take `cluster.TestCluster` as it's only input, and execute running other tests with `cluster.TestCluster.Run()`.

It is worth noting that the tests within the group are executed sequentially and on the same machine. As such, it is not recommended to group tests which modify the system state.

Additionally, the FailFast flag can be enabled during the test registration to skip any remaining steps after a failure has occurred.

Continuing with the look at the `podman` package we can see that `podman.base` is registered like so:

```golang
    register.RegisterTest(&register.Test{
            Run:         podmanBaseTest,
            ClusterSize: 1,
            Name:        `podman.base`,
            Distros:     []string{"rhcos"},
    })
```

If we look at `podmanBaseTest` it becomes very obvious that it's not a test of it's own, but a group of tests.

```go
func podmanBaseTest(c cluster.TestCluster) {
        c.Run("info", podmanInfo)
        c.Run("resources", podmanResources)
        c.Run("network", podmanNetworksReliably)
}
```

## Adding New Packages

If you need to add a new testing package there are few steps that must be done.

1. Create a new directory in `mantle/kola/tests/` which is descriptive of what will be tested.
2. Add at least one file in the new directory with it's package the same name as it's directory name
3. Edit the kola/registry/registry.go file to include your new package
4. Add and register your new tests

As an example, let's say you want to add a new test package called `foo`.

1. First create `mantle/kola/tests/foo/`
2. Then `echo "package foo" > mantle/kola/tests/foo/foo.go`
3. Next, edit `mantle/kola/registry/registry.go` and add this to the imports `_ "github.com/coreos/mantle/kola/tests/foo"`

```golang
package registry

// Tests imported for registration side effects. These make up the OS test suite and is explicitly imported from the main package.
import (
        _ "github.com/coreos/mantle/kola/tests/coretest"
        _ "github.com/coreos/mantle/kola/tests/crio"
        _ "github.com/coreos/mantle/kola/tests/docker"
        _ "github.com/coreos/mantle/kola/tests/etcd"
        _ "github.com/coreos/mantle/kola/tests/foo"
<snip/>
```

4. Lastly, use $EDITOR on `mantle/kola/tests/foo/foo.go` adding in new test groups and tests.

## Full Example

### File: mantle/kola/tests/foo/foo.go
```golang
// Copyright 2019 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package foo

import (
        "github.com/coreos/mantle/kola/cluster"
        "github.com/coreos/mantle/kola/register"
)

// init runs when the package is imported and takes care of registering tests
func init() {
    register.RegisterTest(&register.Test{ // See: https://godoc.org/github.com/coreos/mantle/kola/register#Test
            Run:         exampleTestGroup,
            ClusterSize: 1,
            Name:        `example.example`,
            Flags:       []register.Flag{}, // See: https://godoc.org/github.com/coreos/mantle/kola/register#Flag
            Distros:     []string{"rhcos"},
            FailFast:    true,
    })
}

// exampleTestGroup groups all of the example.example tests together
func exampleTestGroup(c cluster.TestCluster) {
    c.Run("test1", exampleTestOne)
    c.Run("test2", exampleTestTwo)
}

// The first example test (and it does nothing!)
func exampleTestOne(c cluster.TestCluster) {
    // NOOP!
}

// The second example test and it makes sure os-release has content
func exampleTestTwo(c cluster.TestCluster) {
    // Get the first machine in the cluster
    m := c.Machines()[0]
    osrelease := c.MustSSH(m, `cat /etc/os-release`)
    if string(osrelease) == "" {
        c.Errorf("/etc/os-release was empty. Expected content.")
    }
}
```

### File: mantle/kola/registry/registry.go

```golang
package registry

// Tests imported for registration side effects. These make up the OS test suite and is explicitly imported from the main package.
import (
        _ "github.com/coreos/mantle/kola/tests/coretest"
        _ "github.com/coreos/mantle/kola/tests/crio"
        _ "github.com/coreos/mantle/kola/tests/docker"
        _ "github.com/coreos/mantle/kola/tests/etcd"
        _ "github.com/coreos/mantle/kola/tests/flannel"
        _ "github.com/coreos/mantle/kola/tests/foo"
        _ "github.com/coreos/mantle/kola/tests/ignition"
        _ "github.com/coreos/mantle/kola/tests/kubernetes"
        _ "github.com/coreos/mantle/kola/tests/locksmith"
        _ "github.com/coreos/mantle/kola/tests/metadata"
        _ "github.com/coreos/mantle/kola/tests/misc"
        _ "github.com/coreos/mantle/kola/tests/ostree"
        _ "github.com/coreos/mantle/kola/tests/packages"
        _ "github.com/coreos/mantle/kola/tests/podman"
        _ "github.com/coreos/mantle/kola/tests/rkt"
        _ "github.com/coreos/mantle/kola/tests/rpmostree"
        _ "github.com/coreos/mantle/kola/tests/update"
)
```
