# Kola Design
Kola is a framework for testing software integration in CoreOS instances
across multiple platforms. It is primarily designed to operate and
within the CoreOS SDK and test software that has landed in the os image.
Ideally, all software needed for a test should be included by building
it into the image from the SDK.

Kola currently based on qemu/kvm but in the future will support
seamlessly running tests across multiple platforms. Nspawn and EC2 are
the most likely candidates. Tests cannot rely on access to the Internet.

The goal is to focus on platform integration testing and not reproduce
tests accomplished with unit tests. It is possible to move existing test
functionality into Kola platform, but generally, Kola does not aim to
envelope existing test functionality. 

# Roadmap
 * etcd migration test - It would be useful to test the various migration behavior
that exists in the real world as etcd moves from 0.4.7 to 2.0+. This
includes both seeing that etcd 2.0+ calls 0.4.7 when an existing cluster
exists and testing the migration process itself.

 * core-update testing - To test the update process an embedded Omaha
 server that serves an update payload needs to be embedded into Kola
 to replace the python code that exists currently.

 * rkt test suite
