This document describes some of the current high level design
of the project.  It assumes some familiarity with the primary `README.md`.

Builds
----

Conceptually, each run of `coreos-assembler build` may generate a new "build".

By default, a single build will generate both an OSTree commit and a `-qemu.qcow2`.
The OSTree commit data is generated via rpm-ostree, using `src/config/manifest.yaml`.
The QEMU image uses the OSTree commit and the `src/config/image.ks` data, which
defines the partition layout.

There is a single OSTree repository created by `coreos-assembler init`. The new
OSTree commit is written into `repo`; if you have configured a ref in the
manifest, it will be updated.  For more information on OSTree and build systems,
see [the libostree docs](https://ostree.readthedocs.io/en/latest/manual/buildsystem-and-repos/).

The coreos-assembler concept of a "build" binds together an OSTree commit with
one or more images that contain the filesystem tree represented by the commit.

Physically, a coreos-assembler build is represented primarily by a new
subdirectory in `builds/$version`, and inside that directory there's a
`meta.json` that contains a lot of relevant metadata, including the OSTree
commit.

By default, builds are pruned (as is the OSTree repository), although one can
use `build --no-prune` to prevent this.

Change detection
---

All of the filesystem content of a build goes into the ostree commit. Images are
just wrappers for that, containing the partition layout, etc. rpm-ostree has a
lot of built-in intelligence around change detection; if you run
`coreos-assembler build` and the rpm-md repositories haven't changed, and you
haven't edited the manifest, it will simply not generate a new build.

You can detect this situation in a pipeline by comparing `readlink builds/latest`.

However, coreos-assembler builds on top of rpm-ostree and also generates
disk images.  Today, it uses Anaconda, and as mentioned above provides
`image.ks` as input.  coreos-assembler simply checksums that file, and
uses it to support change detection for images as well.

If you want to force a build, use `coreos-assembler build --force`.  A common
reason to do this is when something changes in the tooling itself and you
want that change.

Managing data
----

For production pipelines, the suggested approach is to store data in e.g.
an object store, and sync in the previous latest builds' `meta.json`.  This
is enough for the change detection to kick in.
