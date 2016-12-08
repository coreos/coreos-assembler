# pluton

This repository vendors github.com/coreos/mantle to maintain kola tests focused around coreos distributions of kubernetes and their components rather then tests against all the components of an OS image. The repo is private for now because it may be consuming or vendoring closed source components. It may entirely be possible that we can open-source this at some point and so its best to avoid adding code that would prevent this unless necessary.

The main cmd package of kola was forked while the mantle repo is vendored. This way, we build the kola and kolet directly but can maintain our own set of options that specifying the versions and locations of software that the tests will be testing. The OS tests generally assume all binaries that need testing live on the OS. Here we assume that our CI system is building all the assets that kola will test and passing in their locations at the command line. 

## Contributing
Test writers are needed! Please bug the maintainers to show you how to write a test and to add enough documentation so its not necessary. The smoke test suite is a great place to add tests for one off failures found in our kubernetes distributions.

If the abstractions from the upstream kola packages are not sufficient for our needs we should make upstream changes to github.com/coreos/mantle.

## Vendoring

To start, github.com/coreos/mantle has been manually vendored and its dependencies flattened. If vendor needs become more complex feel free to switch to a vendoring tool of your choice and make a PR.
