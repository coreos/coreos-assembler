# Mantle: Gluing CoreOS together

This repository is a collection of utilities for the CoreOS SDK.

## plume

CoreOS release utility

## plume index

Generate and upload index.html objects to turn a Google Cloud Storage
bucket into a publicly browsable file tree. Useful if you want something
like Apache's directory index for your software download repository.

## kola

Kola is a framework for testing software integration in CoreOS instances
across multiple platforms. It is primarily designed to operate and
within the CoreOS SDK and test software that has landed in the OS image.
Ideally, all software needed for a test should be included by building
it into the image from the SDK.

Kola supports running tests on multiple platforms, currently QEMU and
GCE. In the future systemd-nspawn and EC2 are likely the next to be
added. All local platforms cannot rely on access to the Internet.

The goal is to focus on platform integration testing and not reproduce
tests accomplished with unit tests. It is possible to move existing test
functionality into Kola platform, but generally, Kola does not aim to
envelope existing test functionality.

## ore

Related commands to launch instances on Google Compute Engine(gce)
within the latest SDK image. SSH keys should be added to the gce project
metadata before launching a cluster. All commands have flags that can
overwrite the default project, bucket, and other settings.  `ore help
<command>` can be used to discover all the switches.

### ore upload

Upload latest SDK image to Google Storage and then create a gce image.
Assumes an image packaged with the flag `--format=gce` is present.
Common usage for CoreOS devs using the default bucket and project is:

`ore upload`

### ore list-images

Print out gce images from a project. Common usage:

`ore list-images`

### ore create-instances

Launch instances on gce. SSH keys should be added to the metadata
section of the gce developers console. Common usage:

`ore create-instances -n=3 -image=<gce image name> -config=<cloud config file>`

### ore list-instances

List running gce instances. Common usage:

`ore list-instances`

### ore destroy-instances

Destroy instances on gce based by prefix or name. Images created with
`create-instances` use a common basename as a prefix that can also be
used to tear down the cluster. Common usage:

`ore destroy-instances -prefix=$USER`

