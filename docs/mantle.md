---
has_children: true
has_toc: false
nav_order: 9
---

# Mantle

This repository is a collection of utilities for developing CoreOS systems.
Most of the tools are for uploading, running, and interacting with Fedora
CoreOS and Red Hat CoreOS instances running locally or in a cloud.

Mantle is composed of many utilities:
 - [`kola`](kola.md) for launching instances and running tests
 - [`kolet`](kola.md#kolet) an agent for kola that runs on instances
 - [`ore`](mantle/ore.md) for interfacing with cloud providers
 - [`plume`](mantle/plume.md) for releasing Fedora CoreOS and Fedora Cloud

All of the utilities support the `help` command to get a full listing of their
subcommands and options.
