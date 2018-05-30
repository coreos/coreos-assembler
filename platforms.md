# Mantle platforms

Platforms are an API interface to different environments to run clusters,
create images, collect logging information, etc.

## Authentication

Authentication differs based on the platform. Some platforms like `aws` utilize the
configuration files from their command-line tooling while others define their
own custom configuration format and default locations (like [DigitalOcean](https://github.com/coreos/mantle/tree/master/auth/do.go)).
Generally if any extensions / custom configurations are needed a new file is
created inside of the `auth` package which will define the default location,
the structure of the configuration, and a function to parse the configuration
file (usually named Read<platform>Config and emits a
`map[string]<platform>Config` object).

## API

Platform APIs wrap each cloud provider's golang SDK and live inside of
`platform/api/<platform>/`. There is no direct requirement for what
functionality is present in the API.

## Cluster & Machine

Clusters must implement the `Cluster` [interface](https://github.com/coreos/mantle/tree/master/platform/platform.go#L75-L97).
Machines must implement the `Machine` [interface](https://github.com/coreos/mantle/tree/master/platform/platform.go#L40-L73).

## Adding a new platform to the kola runner

To add a new platform to the `kola` runner the following things must be added:
 1. A platform specific struct inside of `cmd/kola/kola.go` which contains the
 fields that should be logged to give more information about a test run.
 Generally this will contain things like `version` or `ami` and `region`. [This](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L138-L142)
 is an example of the struct and [this](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L179-L183) shows the data
 being added to the output (which can be found in
 `_kola_temp/<platform>-latest/properties.json`).
 2. The platform specific options inside of `cmd/kola/options.go`
 ([for example DigitalOcean](https://github.com/coreos/mantle/tree/master/cmd/kola/kola.go#L179-L183)). The flags will
 generally contain an override for the configuration file location, region,
 type/size, and the image.
 3. The platform needs to be added to the `kolaPlatforms` list inside of
 `cmd/kola/options.go` [here](https://github.com/coreos/mantle/tree/master/cmd/kola/options.go#L32)
 4. The platform options & new cluster inside of `kola/harness.go`. The platform
 options variables are defined [here](https://github.com/coreos/mantle/tree/master/kola/harness.go#L54-L60) and the
 `NewCluster` method is defined [here](https://github.com/coreos/mantle/tree/master/kola/harness.go#L143-L161).

## Other things to consider adding

It is generally preferred that new platforms add garbage collection methods via
the `ore <platform> gc` command.

For platforms which support adding custom images `ore` commands to upload &
create images from the image file.
