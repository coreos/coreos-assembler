---
nav_order: 10
---

# Working with COSA's AI skills
{: .no_toc }

Example tasks to hand to an AI tool (i.e. opencode, goose) leveraging
the skills under .agents/skills. To leverage a skill run `/skills` and
choose a skill and then give a prompt like the examples below:

1. TOC
{:toc}

## Simple Task

    Run the ext.config.upgrade.extended test starting from 44.20260510.2.1. When done look at the
    journal in the logs from the test and tell me how long the rpm-ostree operations took for every boot.

## Intermediate Task

    Do a local build against rawhide, but override the kernel in the build with the kernel build from
    this bodhi update https://bodhi.fedoraproject.org/updates/FEDORA-2026-62bf55e4d8 and run the
    ext.config.security.lockdown test against the built image.

## Advanced Task

    With the local changes to the git repo here compile a new kola. Then write a new external test that
    is a minimal version of ext.config.upgrade.extended but really only does:

    1. leaves zincati disabled
    2. does a skopeo copy of quay.io/fedora/fedora-coreos:stable to an ociarchive
    3. does a manual `sudo rpm-ostree rebase ostree-unverified-image:oci-archive://path/to/ociarchive`

    Then we should run kola (newly compiled) to start an instance at the earliest `stable` stream build
    based on Fedora 43 and run the test.

    I want to run the test with and without one change to benchmark them and compare the differences:

    - updating fsync to false in the ostree repo config (see https://ostreedev.github.io/ostree/man/ostree.repo-config.html)
      inside the instance

    Run this test on AWS m8a.xlarge instances using the credentials in the `aws-creds` file.
