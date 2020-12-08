# The CoreOS Assembler

This is the CoreOS Assembler (often abbreviated COSA) build environment. It is
a collection of various tools used to build [Fedora CoreOS][fcos] style
systems, including RHEL CoreOS. The goal is that everything needed to build and
test the OS comes encapsulated in one (admittedly large) container.

A high level goal of this tool is to support two highly related use cases, and
to keep them as similar as possible:

- Local development ("test a kernel change")
- Be a production build system orchestrated by an external tool (e.g. Jenkins)

See [fedora-coreos-pipeline][pipeline] as an example pipeline.

The container itself is available on [Quay.io](https://quay.io) at
[`quay.io/coreos-assembler/coreos-assembler`][quay-cosa]. It includes the
following tools:

- [`cosa`](docs/cosa.md): entrypoint for the COSA container and dispatcher to other
  commands:
  - To learn how to use COSA, see the
    [Building Fedora CoreOS](docs/building-fcos.md) guide and the
    [Working with CoreOS Assembler](docs/working.md) guide.
  - To learn how to develop on COSA, see the
    [CoreOS Assembler Design](docs/design.md) guide and the
    [Working on CoreOS Assembler](docs/devel.md) guide.
- [`kola`](docs/kola.md): for launching instances and running tests on them
- [`kolet`](docs/kola.md#kolet): an agent for kola that runs on instances
- [`ore`](docs/mantle/ore.md): for interfacing with cloud providers
- [`plume`](docs/mantle/plume.md): for releasing Fedora CoreOS and Fedora Cloud

## Building Fedora CoreOS

See the [Building Fedora CoreOS](docs/building-fcos.md) guide to learn how to
build Fedora CoreOS with COSA.

## Building a custom OS with COSA

For more information about building a custom OS derived or based on Fedora
CoreOS, see [Custom OS](docs/custom.md).

[fcos]: https://coreos.fedoraproject.org
[pipeline]: https://github.com/coreos/fedora-coreos-pipeline
[quay-cosa]: https://quay.io/repository/coreos-assembler/coreos-assembler
