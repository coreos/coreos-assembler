---
name: cosa-dev
description: Develop and test changes to coreos-assembler itself -- build mantle Go binaries (kola, ore, plume), iterate on Python/shell scripts, or do full container rebuilds
---

# Developing coreos-assembler

This skill covers workflows for making and testing changes to
coreos-assembler (COSA) itself, including the Go binaries in `mantle/`,
the Python/shell scripts in `src/`, and the container image.

## Related skills

- Load the **cosa-basics** skill for the `cosa()` bash alias, workdir
  setup, and general build workflow.
- Load the **cosa-kola** skill for running kola tests.
- Load the **cosa-platforms** skill for cloud platform testing.

## Key documentation references

Do NOT duplicate these docs. Read them at runtime for command details:

| File | What it covers |
|------|----------------|
| `docs/devel.md` | Developer workflows: hacking scripts, building Go binaries, container rebuilds |
| `docs/building-fcos.md` | The `cosa()` bash alias definition |
| `Makefile` | Build targets: `mantle`, `kola`, `ore`, `plume`, `kolet`, `install` |
| `mantle/build` | Mantle build script (called by `make kola` etc.) |
| `build.sh` | Container image build steps |
| `Dockerfile` | Container image definition |

## Choosing an approach

| What changed | Approach | Rebuild time |
|---|---|---|
| Go code in `mantle/` only | Build single binary, mount over container copy | ~2 min cold, seconds warm |
| Python/shell in `src/` only | Set `COREOS_ASSEMBLER_GIT` | Instant (no rebuild) |
| Both Go and Python/shell | Combine both approaches | ~2 min cold, seconds warm |
| Dockerfile, RPM deps, or everything | Full `podman build` | Several minutes |

## Approach 1: Modifying mantle Go binaries (kola, ore, plume)

Use this when changing Go code under `mantle/`. The idea is to build the
binary inside the cosa container (which has the Go toolchain), write it
to a host directory, then mount it over the container's installed copy on
subsequent runs.

### Buildable binaries

| Source path | Binary name | Installed at |
|---|---|---|
| `mantle/cmd/kola` | `kola` | `/usr/bin/kola` |
| `mantle/cmd/ore` | `ore` | `/usr/bin/ore` |
| `mantle/cmd/plume` | `plume` | `/usr/bin/plume` |
| `mantle/cmd/kolet` | `kolet` | `/usr/lib/kola/<arch>/kolet` |

### Step 1: Create output and cache directories

```
mkdir -p /tmp/cosa-go-cache /tmp/cosa-bin
chmod 777 /tmp/cosa-go-cache /tmp/cosa-bin
```

The `chmod 777` is needed because the cosa container runs as uid 1000
(`builder`) via `--userns=keep-id`, and the directories must be writable
by that mapped user.

### Step 2: Build the binary

Mount the cosa source tree, the output directory, and optionally a
persistent Go build cache into the container:

```
export COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS="\
  -v=/path/to/coreos-assembler:/srv/cosa-src:ro \
  -v=/tmp/cosa-bin:/srv/cosa-bin:rw \
  -v=/tmp/cosa-go-cache:/home/builder/.cache/go-build:rw"

cosa shell -- bash -c \
  'cd /srv/cosa-src && go build -buildvcs=false -o /srv/cosa-bin/kola ./mantle/cmd/kola'
```

Replace `kola` with `ore` or `plume` as needed. To build all mantle
binaries at once:

```
cosa shell -- bash -c \
  'cd /srv/cosa-src && \
   for cmd in kola ore plume; do \
     go build -buildvcs=false -o /srv/cosa-bin/$cmd ./mantle/cmd/$cmd; \
   done'
```

**Key flags:**

- `-buildvcs=false` -- required to avoid git ownership errors inside the
  container (the source tree is mounted from the host with a different uid).
- The persistent Go build cache (`/home/builder/.cache/go-build`) makes
  subsequent rebuilds incremental. The first build downloads and compiles
  all dependencies (~2 minutes); rebuilds after a small code change take
  only a few seconds.

### Step 3: Quick compilation check (no binary output)

To verify that the code compiles without producing a binary:

```
cosa shell -- bash -c \
  'cd /srv/cosa-src && go build -buildvcs=false ./mantle/...'
```

### Step 4: Test with the modified binary

Mount the built binary over the container's installed copy using
`COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS`:

```
export COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS="\
  -v=/tmp/cosa-bin/kola:/usr/bin/kola:ro"

cosa kola run -p qemu basic
```

This can be combined with other mounts. For example, to also mount
AWS credentials:

```
export COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS="\
  -v=/tmp/cosa-bin/kola:/usr/bin/kola:ro \
  -v=/path/to/aws-creds:/srv/aws-creds:ro"

cosa kola run -p aws --aws-credentials-file /srv/aws-creds basic
```

### Step 5: Iterate

After making further code changes, re-run the build command from Step 2.
With the persistent Go cache, only the changed packages are recompiled.
Then re-run the test from Step 4 -- the mount picks up the new binary
automatically.

## Approach 2: Modifying Python/shell scripts (src/)

Use this when changing files under `src/` (e.g. `src/cmd-build`,
`src/cmd-init`, `src/cosalib/*.py`). No rebuild is needed.

Set the `COREOS_ASSEMBLER_GIT` environment variable to point at your
local coreos-assembler checkout. The `cosa()` bash alias will
automatically mount `$COREOS_ASSEMBLER_GIT/src/` over
`/usr/lib/coreos-assembler/` inside the container:

```
export COREOS_ASSEMBLER_GIT=/path/to/coreos-assembler
cosa init https://github.com/coreos/fedora-coreos-config
cosa build
```

Changes to files under `src/` take effect immediately on the next `cosa`
invocation. No rebuild or remount is required.

**Limitation:** This only covers files installed to
`/usr/lib/coreos-assembler/` (i.e. `src/` contents). It does NOT affect
Go binaries (`kola`, `ore`, `plume`) or system packages. For Go changes,
combine this with Approach 1.

### Combining with Go binary changes

```
export COREOS_ASSEMBLER_GIT=/path/to/coreos-assembler
export COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS="\
  -v=/tmp/cosa-bin/kola:/usr/bin/kola:ro"

cosa kola run basic
```

This uses the modified Python/shell scripts from `COREOS_ASSEMBLER_GIT`
AND the modified kola binary from the mount.

## Approach 3: Full container rebuild

Use this when changing the `Dockerfile`, adding or updating RPM
dependencies, or when you want to verify everything works together as a
complete image.

### Build the container image

From the coreos-assembler repo root:

```
podman build -t localhost/coreos-assembler .
```

To speed things up by reusing the official image as a base (useful when
only cosa code changed, not dependencies):

```
podman build -t localhost/coreos-assembler . \
  --from quay.io/coreos-assembler/coreos-assembler:latest
```

### Use the locally-built container

Set `COREOS_ASSEMBLER_CONTAINER` so the `cosa()` alias uses your local
image instead of the upstream one:

```
export COREOS_ASSEMBLER_CONTAINER=localhost/coreos-assembler
cosa init https://github.com/coreos/fedora-coreos-config
cosa build
```

This is the slowest approach but the most complete. It is needed when:

- The `Dockerfile` or `build.sh` changed.
- System RPM dependencies were added or updated.
- You want a final integration check before submitting a PR.

## Important notes

- The `cosa()` bash alias creates a **transient container** for each
  invocation. All state persists in the cosa workdir (mounted at
  `/srv/`), not inside the container. This is why mounting files in via
  `COREOS_ASSEMBLER_CONTAINER_RUNTIME_ARGS` works -- each run gets the
  latest version of whatever is mounted.
- Use `cosa shell` to get a persistent interactive session inside the
  container. This is useful for running multiple build/test commands
  without container startup overhead. However, be aware that mounted
  binaries are fixed at container start time.
- The Go build cache directory inside the container is
  `/home/builder/.cache/go-build`. Persisting it to the host avoids
  cold rebuilds.
- When mounting binaries with `:ro`, the container cannot modify them.
  This is intentional -- it prevents accidental overwrites.
