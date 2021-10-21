---
nav_order: 6
---

# CoreOS Assembler Design
{: .no_toc }

This document describes some of the current high level design of the project.
It assumes some familiarity with the primary `README.md`.

1. TOC
{:toc}

## Builds

coreos-assembler operates on a "build directory", which can contain multiple
builds. A build is a pairing of an OSTree commit (stored as `*-ostree.tar`) as
well as an optional set of disk images.

This is in contrast to [rpm-ostree](https://coreos.github.io/rpm-ostree/) which
just generates OSTree commits, and doesn't have anything to do with disk
images. Another way to say this is that coreos-assembler ties together OSTree
commits with disk images under a single build schema, and gives them the same
version numbering for example.

The default for `cosa build` is to generate a new OSTree commit and a `qemu`
image. This supports e.g. `cosa run`.

The OSTree commit data is generated via rpm-ostree, using
`src/config/manifest.yaml`. Image configuration uses `src/config/image.yaml`.

Physically, a coreos-assembler build is represented primarily by a new
subdirectory in `builds/$version`, and inside that directory there's a
`meta.json` that contains a lot of relevant metadata, including the OSTree
commit.

There is also a `builds/builds.json` which maintains the list of builds.  The
reason for this is that HTTP doesn't offer a way to enumerate a directory.

After a build is generated there are a variety of `buildextend-$x` commands,
for example `buildextend-ec2` which can upload to AWS, and `buildextend-metal`
which generates a bare metal disk image.

By default, builds are pruned (as is the OSTree repository), although one can
use `build --no-prune` to prevent this.

For more information on OSTree and build systems, see [the libostree
docs](https://ostreedev.github.io/ostree/buildsystem-and-repos/).

## Change detection

All of the filesystem content of a build goes into the ostree commit. Images
are just wrappers for that, containing the partition layout, etc. rpm-ostree
has a lot of built-in intelligence around change detection; if you run
`coreos-assembler build` and the rpm-md repositories haven't changed, and you
haven't edited the manifest, it will simply not generate a new build.

You can detect this situation in a pipeline by comparing `readlink
builds/latest`.

However, coreos-assembler builds on top of rpm-ostree and also generates disk
images. It uses supermin to run a virtual machine that runs code to write the
ostree content along with the filesystem layout into a disk image.

If you want to force a build, use `coreos-assembler build --force`.  A common
reason to do this is when something changes in the tooling itself and you want
that change.

## Managing data

cosa offers `buildfetch` which downloads builds from `https://` or `s3://`, as
well as a `buildupload` which is oriented around S3 today. However, there are
a wide variety of S3-compatible storage systems, so you are not tied to AWS.
