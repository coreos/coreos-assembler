---
title: Building a custom OS with CoreOS Assembler
nav_order: 3
---

# Using coreos-assembler to build custom FCOS derivatives

The primary goal of Fedora CoreOS is to be useful directly, and
to have users deploy containers for applications, etc.

Some people and organizations have asked about creating custom
derivatives of Fedora CoreOS using this project.  This is supported
on a best-effort basis.  Supporting these use cases is not a primary
goal of coreos-assembler, though we try not to purposely break them.
Also note that coreos-assembler is still evolving, and it's likely
that there will occasionally be breaking changes. You should
subscribe to releases to make sure you're aware of these.
That said, we do try to maintain a stable interface between
coreos-assembler and the "source config" repo.

Maintaining a custom operating system build is a nontrivial undertaking
that you shouldn't take lightly.  But we will make efforts to
document changes and keep coreos-assembler relatively stable.

coreos-assembler is used to build RHEL CoreOS, but coreos-assembler
is not a Red Hat product at the current time.
