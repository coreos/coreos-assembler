# k8s-kola

This repository vendors github.com/coreos/mantle purely to maintain kola tests related to booktkube and the tectonic controllers. This was broken out into a separate repository because kola doesn't currently support test libraries to be built separetly from itself. Also because these tests necessarily use closed source components.

The main packages of kola are forked while all mantle libraries are vendored. This way we still build the kola and kolet directly and can maintain our own subcommand that has flags for all the input required for the bootkube tests. The OS tests generally assume all binaries that need testing live on the OS (or sometimes containers). Here we assume that jeknins is building all the assets we wish to test. Since these assets should be mostly in common among the tests we can just add a sub-command to kola

## Future of this repo

It is intended that some of these tests related to bootkube are broken away from the closed source tests of controllers and placed in the OS maintained test suite. Additionally, if plugin support is added to kola we will move to using that and building kola itself directly from the main coreos/mantle branch. 

## Vendoring

For the initial start I have manually vendored github.com/coreos/mantle and flattened the dependancies. If our vendor needs become more complex feel free to switch to a vendoring tool of your choice and make a PR.
