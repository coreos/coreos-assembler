---
name: cosa-basics
description: Initialize a COSA workdir, build or fetch Fedora CoreOS images, and launch local QEMU VMs using coreos-assembler
---

# COSA Basics

This skill guides you through the core coreos-assembler (COSA) workflow:
initializing a build directory, obtaining a CoreOS build (either from source
or by fetching a published build), and launching a local QEMU VM.

## Out of scope

Cloud platform image building and uploading (AWS, GCP, Azure, etc.) are not
covered here. Load the **cosa-platforms** skill for those workflows.

## Key documentation references

Do NOT duplicate these docs. Read them at runtime for command details:

| File | What it covers |
|------|----------------|
| `docs/building-fcos.md` | Full build walkthrough, prerequisites, the `cosa()` bash alias |
| `docs/cosa.md` | Command reference overview (all cosa subcommands) |
| `docs/cosa/run.md` | Detailed `cosa run` and QEMU VM options |
| `docs/working.md` | Overrides, customization, advanced usage |
| `docs/devel.md` | Running cosa locally outside a container |
| `src/cmd-init` | `cosa init` source -- all supported flags |
| `src/cmd-buildfetch` | `cosa buildfetch` source -- all supported flags |

## Running COSA

### Container mode (default)

COSA is designed to run inside a container. The container image is:

```
quay.io/coreos-assembler/coreos-assembler
```

Before running any cosa commands, read `docs/building-fcos.md` and extract the
`cosa()` bash alias function defined there. This alias wraps `podman run` with
the necessary privileges, device mounts (`/dev/kvm`, `/dev/fuse`), user
namespace mapping, and volume mounts. The current working directory is mounted
as `/srv/` inside the container.

Set up the alias in the user's shell before running any cosa commands.

### Local mode (for cosa developers)

Developers who have cosa installed locally can run commands directly without
the container wrapper. Read `docs/devel.md` for details on this workflow. The
commands themselves are the same; only the execution environment differs.

## Workflow

### Step 1: Set up a COSA workdir

1. Ask the user for a working directory path. Default to `./fcos` if they
   don't specify one.
2. If the directory does not exist, create it.
3. `cd` into the directory.
4. Set up the `cosa()` bash alias (read it from `docs/building-fcos.md`).
5. Initialize the workdir:

```
cosa init https://github.com/coreos/fedora-coreos-config
```

The user can override the config repo URL. Additional useful flags (read
`src/cmd-init` for the full list):

- `--branch BRANCH` -- use a specific branch of the config repo
- `--variant VARIANT` -- select a build variant
- `--force` -- allow init in a non-empty directory

After init, the directory structure will contain `src/config/` (the cloned
config repo), plus `builds/`, `cache/`, `tmp/`, and `overrides/` directories
that are populated during builds.

### Step 2: Obtain a build

Choose one of the following paths based on what the user wants to do.

#### Option A: Build from source

This builds a fresh CoreOS image from the config repo and RPM sources:

```
cosa build
cosa osbuild qemu
```

`cosa build` creates the bootable OCI container (ostree commit). `cosa osbuild
qemu` derives a QEMU disk image from that container. Both are required before
`cosa run` will work.

Note: `cosa fetch` is a no-op in the current build flow. Package fetching
happens automatically inside `cosa build` via buildah.

#### Option B: Fetch an existing published build

This downloads a previously published build from the Fedora CoreOS build
servers. Use this when you want to test or inspect an existing release without
building from source.

**Default flags:** Always include `--artifact qemu --decompress` unless the
user specifically asks for a different platform artifact. The published
artifacts are stored compressed on the server; `--decompress` expands them so
`cosa run` can use the resulting qcow2 directly.

**Fetch the latest build for a stream:**

```
cosa buildfetch --stream <stream> --artifact qemu --decompress
```

Available streams: `stable`, `testing`, `testing-devel`, `next`, `rawhide`

If the user does not specify a stream, default to `stable`.

**Fetch a specific build by ID:**

```
cosa buildfetch -b <build-id> --artifact qemu --decompress
```

When `-b`/`--build` is given, `--stream` is not required -- the stream is
auto-detected from the build ID's version number (the third dotted component
maps to a stream, e.g. `.2.` = `testing`, `.3.` = `stable`). Read
`src/cmd-buildfetch` for the full version-to-stream mapping. If `--stream`
is also provided it must match or the command will error.

**If the user wants a non-qemu artifact:** replace `--artifact qemu` with the
appropriate artifact name (e.g., `metal`, `aws`, `gcp`). But note that cloud
platform workflows are better served by a dedicated cosa-cloud skill.

### Step 3: Launch the VM

```
cosa run
```

This launches a QEMU VM using the latest build image with `-snapshot` (changes
are discarded on exit). By default it establishes an SSH connection with
auto-login as the `core` user.

**Common options** (read `docs/cosa/run.md` for the full list):

| Flag | Purpose |
|------|---------|
| `-c` / `--devshell-console` | Use serial console instead of SSH (see full boot, GRUB menu) |
| `--qemu-image PATH` | Use a specific qcow2 image instead of the latest build |
| `-B` / `--butane PATH` | Pass a Butane config for the VM |
| `-i` / `--ignition PATH` | Pass an Ignition config for the VM |
| `--add-disk SIZE[:OPTS]` | Attach additional disks |
| `--kargs 'ARGS'` | Append kernel arguments |
| `-m SIZE` | Set VM memory in MB |

**Exiting the VM:**

- SSH mode: `exit` or `Ctrl-D`
- Console mode: `Ctrl-a x`

## Important notes

- Each `cosa` command (via the alias) creates a transient container. Use
  `cosa shell` for a persistent interactive session inside the container.
- `/dev/kvm` is required for both building disk images and running VMs. The
  host must be bare metal or have nested virtualization enabled.
- The build directory at `$PWD` is mounted at `/srv/` inside the container.
  Build artifacts persist across container invocations.
- The workdir's `builds/latest` symlink always points to the most recent build.
- To run kola tests against a build, load the **cosa-kola** skill. It covers
  test discovery, `cosa kola run`, upgrade tests, and result inspection.
