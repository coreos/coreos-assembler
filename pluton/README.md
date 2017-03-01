# pluton
Pluton represents a tool to enable testing of kubernetes clusters built upon the kola testing primitives. Each test in pluton receives a working kubernetes cluster to test against rather then a `kola.TestCluster`. The spawn package is the glue that utilizes the platform package to build a kubernetes cluster from a tool. Right now, bootkube on gce is the primarily supported kubernetes platform. 

## Examples
Building:
`./build`

Listing Available Tests:
`./bin/pluton list`

Running the main bootkube test suite:
```
./bin/pluton run \
--parallel 5 \
--platform=gce \
--gce-image=projects/coreos-cloud/global/images/coreos-stable-1235-12-0-v20170223 \
--bootkubeRepo=$IMAGE_REPO \
--bootkubeTag=$IMAGE_TAG \
bootkube*
```

Running a bootkube conformance test:

```
./bin/pluton run \
--parallel 5 \
--platform=gce \
--gce-image=projects/coreos-cloud/global/images/coreos-stable-1235-12-0-v20170223 \
--bootkubeRepo=$IMAGE_REPO \
--bootkubeTag=$IMAGE_TAG \
--hostKubeletTag=v1.5.3_coreos.0 \
--conformanceVersion=v1.5.3+coreos.0 \
conformance*
```

Getting Logs:
By default, journal logs for each machine per test will be placed in `_pluton_temp` and overwritten on the next invocation of pluton in the same directory.



## Roadmap
 - Directly use new harness pkg such that a `pluton.Cluster` is passed to every test function
 - Begin to build out the ability of tests to register options in the test structure that customize use of the spawn package
 - build a subcommand that looks like `pluton daemon [options] ./custom_script` in which the custom script is passed the location of a temporary kubeconfig. This will enable use of pluton in other repositories that just rely on a kubeconfig and a single cluster and don't wish to integrate and register tests in to the harness directly
 - Research allowing different implementations of the spawn package.
 - Collect docker logs automatically for each machine.
 - 

### daemon plan

The goal of the daemon subcommand is to allow easier integration and use of pluton as a CI tool for creating and destroying bootkube based clusters for those that already have kubernetes tests and don't need direct harness integration that runs multiple clusters at once. It will look like: 

`pluton daemon [-options] ./test.sh`

This would create a cluster and exec `./test.sh` which,  for many people,  would just be calling a go test suite that only needs a KUBECONFIG. It would export a KUBECONFIG env to `./test.sh` and tear down the cluster when the script is done.

The daemon would also have an interactive mode `pluton daemon [-options] --interactive` which would run a cluster, drop out a kubeconfig and eventually SSH keys as well. Issueing  a sigterm would then gracefully shutdown the cluster.

The `[-options]` command right now would generally specify gce cloud platform options and the bootkube container to use, for example:

```
--platform=gce \
--gce-image=projects/coreos-cloud/global/images/coreos-stable-1235-12-0-v20170223 \
--bootkubeRepo=quay.io/coreos/bootkube \
--bootkubeTag=v0.3.9 \
--hostKubeletTag=v1.5.3+coreos.0
```

The main limitation is that right now you would specify a bootkube container to use and you have no choice of cloud platform. Internally there are the right interfaces to swap in something like terraform cloud platform. In the future we also want the ability to specify additional cluster assets. Overtime we want better ways to specify all aspects of how the initial kubernetes cluster is built and what versions of what software it is running.
