---
name: cosa-platforms
description: Build disk images, upload to cloud providers, and run tests or spawn instances on non-QEMU platforms (AWS, GCP, Azure, ISO, metal, etc.)
---

# Platform Images and Cloud Testing with cosa

This skill covers working with non-QEMU platforms in coreos-assembler:
building platform-specific disk images, uploading them to cloud providers,
running kola tests on cloud platforms, and spawning interactive cloud
instances.

## Related skills

- Load the **cosa-basics** skill for workdir setup, the `cosa()` bash alias,
  and general build workflow.
- Load the **cosa-kola** skill for general kola test mechanics (test
  discovery, result inspection, common flags, external tests).
- This skill adds the platform-specific details on top of those.

## Key documentation references

Do NOT duplicate these docs. Read them at runtime for command details:

| File | What it covers |
|------|----------------|
| `docs/mantle/credentials.md` | Credential setup for all platforms (AWS, Azure, GCP, OpenStack, DO, ESX, etc.) |
| `docs/cosa.md` | Command reference including `osbuild` and `imageupload-*` |
| `docs/cosa/imageupload-aws.md` | AWS AMI upload (IAM permissions, credentials) |
| `docs/cosa/osbuild-secex.md` | IBM Secure Execution image building |
| `docs/cosa/run.md` | `cosa run` (QEMU-only) and `kola spawn` for cloud instances |
| `docs/kola.md` ("Run tests on cloud platforms") | Cloud testing examples for AWS, GCP, Azure |
| `mantle/cmd/kola/options.go` | Source of truth for all platform-specific flags |

## Supported platforms

Kola supports the following platforms via the `-p`/`--platform` flag:

| Platform | `-p` value | Image flag | Default credentials |
|----------|-----------|------------|-------------------|
| AWS | `aws` | `--aws-ami` | `~/.aws/credentials` |
| Azure | `azure` | `--azure-disk-uri` | `~/.azure/azureCreds.json` |
| DigitalOcean | `do` | `--do-image` | `~/.config/digitalocean.json` |
| ESX/VMware | `esx` | `--esx-base-vm` | `~/.config/esx.json` |
| GCP | `gcp` | `--gcp-image` | GCP service account JSON key |
| OpenStack | `openstack` | `--openstack-image` | `~/.config/openstack.json` |
| QEMU | `qemu` | `--qemu-image` | (none) |
| QEMU ISO | `qemu-iso` | `--qemu-iso` | (none) |

Read `docs/mantle/credentials.md` for full credential setup details per
platform, and `mantle/cmd/kola/options.go` or `cosa kola run -h` for the
complete flag list.

## Workflow

### Step 1: Build the platform image

```
cosa osbuild <platform>
```

Use `cosa osbuild --supported-platforms` to list all available platforms.
Multiple platforms can be built in one invocation:

```
cosa osbuild aws azure gcp
```

This creates disk images in the appropriate format for each platform (e.g.
VMDK for AWS, VHD for Azure, tar.gz for GCP, raw for metal).

### Step 2: Upload to the cloud provider

For cloud platforms that require image upload:

```
cosa imageupload-<platform>
```

Available upload targets: `aliyun`, `aws`, `azure`, `gcp`, `powervs`.

Read `docs/cosa/imageupload-aws.md` for AWS-specific IAM and credential
setup. Not all platforms have an `imageupload-*` command; for those that
don't, use `ore` directly or upload manually.

### Step 3: Set up credentials

Read `docs/mantle/credentials.md` for per-platform credential configuration.
Each platform has its own credential file format and default location (see
the table above).

### Step 4: Run tests on a cloud platform

```
cosa kola run -p <platform> [platform-specific flags] <test-pattern>
```

Examples:

```
# AWS
cosa kola run -p aws --aws-ami <AMI> --aws-region us-east-1 basic

# GCP
cosa kola run -p gcp --gcp-image <image> --gcp-json-key <key> --gcp-project <project> basic

# Azure
cosa kola run -p azure --azure-credentials <file> --azure-disk-uri <URI> basic
```

Read `docs/kola.md` ("Run tests on cloud platforms") for more examples.
For the full list of flags per platform, read `mantle/cmd/kola/options.go`
or run `cosa kola run -h`.

### Step 5: Spawn instances for interactive debugging

```
cosa kola spawn -p <platform> [platform-specific flags]
```

This launches an instance and opens an SSH shell without running tests.
Useful for interactive debugging. Key flags:

| Flag | Purpose |
|------|---------|
| `--shell` / `-s` | Open an SSH shell (default: true) |
| `--detach` / `-t` | Spawn in the background (implies `--shell=false --remove=false --verbose --keys`) |
| `--remove=false` | Keep the instance running after the shell exits |
| `--keys` / `-k` | Inject SSH keys from the agent and `~/.ssh/` |
| `--nodecount` / `-c` | Number of instances to spawn |
| `--userdata` / `-u` | Path to Ignition/Butane config for the instance |
| `--ssh-command` / `-x` | Run a command over SSH instead of opening a shell |

Note: `--reconnect` is only available on QEMU platforms.

Read `docs/cosa/run.md` ("Running on cloud platforms") for more details.

## Important notes

- `cosa run` is QEMU-only. For all other platforms, use
  `cosa kola run -p <platform>` (for tests) or
  `cosa kola spawn -p <platform>` (for interactive instances).
- When a cosa build exists in the workdir, kola auto-fills some platform
  image references from the build metadata (e.g. AMI for AWS, image name
  for GCP). This means you may not need to specify `--aws-ami` etc. if the
  build already has that info in `meta.json`.
- Instance types default to reasonable values per platform and architecture
  (e.g. `m5.large` for AWS x86_64, `c6g.xlarge` for aarch64). Override with
  platform-specific type flags (e.g. `--aws-type`, `--gcp-machinetype`,
  `--azure-size`).
