# pluton
Pluton represents a tool to enable testing of kubernetes clusters build upon the kola testing primitives. Each test in pluton recieves a working kubernets cluster to test against rather then a `kola.TestCluster`. The spawn package is the glue that utilizes the platform package to build a kubernetes cluster from a tool. Right now, Bootkube on gce is the primarily supported kubernets platform. 

## Roadmap
 - Directly use new harness pkg such that a `pluton.Cluster` is passed to every test function
 - Begin to build out the ability of tests to register options in the test structure that customize use of the spawn package
 - build a subcommand that looks like `pluton daemon [options] ./custom_script` in which the custom script is passed the location of a temporary kubeconfig. This will enable use of pluton in other repositories that just rely on a kubeconfig and a single cluster and don't wish to integrate and register tests in to the harness directly
 - Research allowing different implementations of the spawn package.
