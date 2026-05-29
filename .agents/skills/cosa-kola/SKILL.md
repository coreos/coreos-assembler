---
name: cosa-kola
description: Run kola tests against CoreOS builds using coreos-assembler, including native and external tests
---

# Running Tests with cosa kola

This skill guides you through running kola tests against CoreOS builds using
coreos-assembler.

## Out of scope

Writing new native (Go) kola tests and cloud platform testing are not covered
here. For writing external tests, refer to the docs listed below.

## Key documentation references

Do NOT duplicate these docs. Read them at runtime for command details:

| File | What it covers |
|------|----------------|
| `docs/kola.md` | Kola overview, `cosa kola` wrapper behavior, automatic test discovery, common flags, denylists, test output logs |
| `docs/kola/external-tests.md` | How to write and structure external tests, `kola.json` metadata, reboot support |
| `docs/kola/adding-tests.md` | Adding native Go tests to kola (for reference) |
| `src/cmd-kola` | `cosa kola` wrapper source -- shows how args are passed through to kola |

## Prerequisites

Before running tests you need a QEMU image in the cosa workdir and the
`cosa()` bash alias must be set up. Load the **cosa-basics** skill for both
of these -- it covers container invocation, building from source, and fetching
published builds.

## Workflow

All commands below assume you are in a cosa workdir (or inside the cosa
container). Read `docs/kola.md` for full flag documentation.

### Step 1: Find the right test(s)

List available tests to find the one the user wants:

```
cosa kola list
```

For JSON output with descriptions:

```
cosa kola list --json | jq -r '.[] | [.Name, .Description] | @tsv'
```

External tests from `src/config/tests/kola/` are discovered automatically
and named `ext.config.<path>` (e.g., `ext.config.files.aleph-version`).

### Step 2: Run the test(s)

Test names are glob patterns:

```
cosa kola run basic
cosa kola run ext.config.files.aleph-version
cosa kola run 'ext.config.*'
```

Running `cosa kola run` with no pattern runs all tests (can take a long time).

Read `docs/kola.md` for flags like `--parallel`, `--tag`, `--rerun`,
`--denylist-test`, `--ssh-on-test-failure`, `--append-butane`, and others.

### Step 3: Check results

Test logs are written to `tmp/kola/<test-name>/`. Key files: `journal.txt`,
`console.txt`, `ignition.json`, `journal-raw.txt.gz`. Read `docs/kola.md`
("More information on tests") for details.

## Special test types

### ISO install tests

ISO install tests are regular kola tests (the `testiso` subcommand was removed
in https://github.com/coreos/coreos-assembler/pull/4377). Run them with
`cosa kola run` like any other test:

```
cosa kola run iso.iso-offline-install.mpath.bios
cosa kola run 'iso.*'
```

### Upgrade tests

```
cosa kola --upgrades
```

This runs the `run-upgrade` subcommand, which tests OS upgrades. It
automatically finds parent images and caches them in `tmp/kola-qemu-cache/`.
