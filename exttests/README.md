# POC for gathering upstream tests into a container

We landed support for "exttests" in coreos-assembler:
https://github.com/coreos/coreos-assembler/blob/98d40e6bb13adc02bcd5f02f1d5bff7bafa0780d/mantle/kola/README-kola-ext.md

Since then we are successfully running upstream test suites using
kola on upstream pull requests.

But we also want the inverse: run upstream tests on builds outside
of PRs to those repos.

For example, I really want to run ostree's "transactionality"
test on FCOS builds so that if a kernel change breaks it, we
know that.

## Proposal

- Build this container in api.ci like we're building the cosa buildroot;
  currently done at `registry.svc.ci.openshift.org/cgwalters/cosa-exttests:latest`
- Change the FCOS/RHCOS pipelines to pull this container and do: `kola run ... ext.*`
  (This raises some issues like the fact that we'd need to store/share the images
   in a way that would allow a secondary container to access them)

### Alternative: coreos-assembler only

Fold this into coreos-assembler.

### Alternative: Make RPMs of tests and install those in coreos-assembler

We have an `ostree-tests` RPM...but I'm not sure I want to deal with
packaging for it.
