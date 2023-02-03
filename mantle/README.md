# Mantle: Gluing Container Linux together

This repository is a collection of utilities for developing Container Linux. Most of the
tools are for uploading, running, and interacting with Container Linux instances running
locally or in a cloud.

## Overview
Mantle is composed of many utilities:
 - `kola` for launching instances and running tests
 - `kolet` an agent for kola that runs on instances
 - `ore` for interfacing with cloud providers
 - `plume` for releasing Container Linux

All of the utilities support the `help` command to get a full listing of their subcommands
and options.

## Tools

### kola
Kola is a framework for testing software integration in Container Linux instances
across multiple platforms. It is primarily designed to operate within
the Container Linux SDK for testing software that has landed in the OS image.
Ideally, all software needed for a test should be included by building
it into the image from the SDK.

Kola supports running tests on multiple platforms, currently QEMU, GCP,
AWS, VMware VSphere, Packet, and OpenStack. In the future systemd-nspawn and other
platforms may be added.
Local platforms do not rely on access to the Internet as a design
principle of kola, minimizing external dependencies. Any network
services required get built directly into kola itself. Machines on cloud
platforms do not have direct access to the kola so tests may depend on
Internet services such as discovery.etcd.io or quay.io instead.

Kola outputs assorted logs and test data to `_kola_temp` for later
inspection.

Kola is still under heavy development and it is expected that its
interface will continue to change.

By default, kola uses the `qemu` platform with the most recently built image
(assuming it is run from within the SDK).

#### kola run
The run command invokes the main kola test harness. It
runs any tests whose registered names matches a glob pattern.

`kola run <glob pattern>`

`--denylist-test` can be used if one or more tests in the pattern should be skipped.
This switch may be provided once:

`kola --denylist-test linux.nfs.v3 run`

multiple times:

`kola --denylist-test linux.nfs.v3 --denylist-test linux.nfs.v4 run`

and can also be used with glob patterns:

`kola --denylist-test linux.nfs* --denylist-test crio.* run`

#### kola list
The list command lists all of the available tests.

#### kola spawn
The spawn command launches Container Linux instances.

#### kola mkimage
The mkimage command creates a copy of the input image with its primary console set
to the serial port (/dev/ttyS0). This causes more output to be logged on the console,
which is also logged in `_kola_temp`. This can only be used with QEMU images and must
be used with the `coreos_*_image.bin` image, *not* the `coreos_*_qemu_image.img`.

#### kola bootchart
The bootchart command launches an instance then generates an svg of the boot process
using `systemd-analyze`.

#### kola updatepayload
The updatepayload command launches a Container Linux instance then updates it by
sending an update to its update_engine. The update is the `coreos_*_update.gz` in the
latest build directory.

#### kola subtest parallelization
Subtests can be parallelized by adding `c.H.Parallel()` at the top of the inline function
given to `c.Run`. It is not recommended to utilize the `FailFast` flag in tests that utilize
this functionality as it can have unintended results.

#### kola test namespacing
The top-level namespace of tests should fit into one of the following categories:
1. Groups of tests targeting specific packages/binaries may use that
namespace (ex: `docker.*`)
2. Tests that target multiple supported distributions may use the
`coreos` namespace.
3. Tests that target singular distributions may use the distribution's
namespace.

#### kola test registration
Registering kola tests currently requires that the tests are registered
under the kola package and that the test function itself lives within
the mantle codebase.

Groups of similar tests are registered in an init() function inside the
kola package.  `Register(*Test)` is called per test. A kola `Test`
struct requires a unique name, and a single function that is the entry
point into the test. Additionally, userdata (such as a Container Linux
Config) can be supplied. See the `Test` struct in
[kola/register/register.go](https://github.com/coreos/coreos-assembler/blob/main/mantle/kola/register/register.go)
for a complete list of options.

#### kola test writing
A kola test is a go function that is passed a `platform.TestCluster` to
run code against.  Its signature is `func(platform.TestCluster)`
and must be registered and built into the kola binary. 

A `TestCluster` implements the `platform.Cluster` interface and will
give you access to a running cluster of Container Linux machines. A test writer
can interact with these machines through this interface.

To see test examples look under
[kola/tests](https://github.com/coreos/coreos-assembler/blob/main/mantle/kola/tests) in the
mantle codebase.

For a quickstart see [kola/README.md](kola/README.md).

#### kola native code
For some tests, the `Cluster` interface is limited and it is desirable to
run native go code directly on one of the Container Linux machines. This is
currently possible by using the `NativeFuncs` field of a kola `Test`
struct. This like a limited RPC interface.

`NativeFuncs` is used similar to the `Run` field of a registered kola
test. It registers and names functions in nearby packages.  These
functions, unlike the `Run` entry point, must be manually invoked inside
a kola test using a `TestCluster`'s `RunNative` method. The function
itself is then run natively on the specified running Container Linux instances.

For more examples, look at the
[coretest](https://github.com/coreos/coreos-assembler/tree/main/mantle/kola/tests/coretest)
suite of tests under kola. These tests were ported into kola and make
heavy use of the native code interface.

#### Manhole
The `platform.Manhole()` function creates an interactive SSH session which can
be used to inspect a machine during a test.

The `--ssh-on-test-failure` flag can be specified to have the kola runner
automatically SSH into a machine when any `MustSSH` calls fail.

### kolet
kolet is run on kola instances to run native functions in tests. Generally kolet
is not invoked manually.

### ore
Ore provides a low-level interface for each cloud provider. It has commands
related to launching instances on a variety of platforms (gcloud, aliyun, aws,
azure, esx, ibmcloud and packet) within the latest SDK image. Ore mimics the underlying
api for each cloud provider closely, so the interface for each cloud provider
is different. See each providers `help` command for the available actions.

Note, when uploading to cloud platforms, be sure to use the image built for
that particular platform (with `cosa buildextend-...`).

### plume
Plume is the Container Linux release utility. Releases are done in two stages,
each with their own command: pre-release and release. Both of these commands are idempotent.

#### plume pre-release
The pre-release command does as much of the release process as possible without making anything public.
This includes uploading images to cloud providers (except those like gcp which don't allow us to upload
images without making them public).

### plume release
Publish a new Container Linux release. This makes the images uploaded by pre-release public and uploads
images that pre-release could not. It copies the release artifacts to public storage buckets and updates
the directory index.

#### plume index
Generate and upload index.html objects to turn a Google Cloud Storage
bucket into a publicly browsable file tree. Useful if you want something
like Apache's directory index for your software download repository.
Plume release handles this as well, so it does not need to be run as part of
the release process.

## Platform Credentials
Each platform reads the credentials it uses from different files. The `aliyun`, `aws`, `azure`, `do`, `esx`, `ibmcloud` and `packet`
platforms support selecting from multiple configured credentials, call "profiles". The examples below
are for the "default" profile, but other profiles can be specified in the credentials files and selected
via the `--<platform-name>-profile` flag:
```
kola spawn -p aws --aws-profile other_profile
```

### aliyun
`aliyun` reads the `~/.aliyun/config.json` file used by Aliyun's aliyun command-line tool.
It can be created using the `aliyun` command:
```
$ aliyun configure
```
To configure a different profile, use the `--profile` flag
```
$ aliyun configure --profile other_profile
```

The `~/.aliyun/config.json` file can also be populated manually:
```
{
  "current": "",
  "profiles": [
    {
      "name": "",
      "mode": "AK",
      "access_key_id": "ACCESS_KEY_ID",
      "access_key_secret": "ACCESS_KEY_SECRET",
      "sts_token": "",
      "ram_role_name": "",
      "ram_role_arn": "",
      "ram_session_name": "",
      "private_key": "",
      "key_pair_name": "",
      "expired_seconds": 0,
      "verified": "",
      "region_id": "eu-central-1",
      "output_format": "json",
      "language": "zh",
      "site": "",
      "retry_timeout": 0,
      "retry_count": 0
    }
  ]
}
```

### aws
`aws` reads the `~/.aws/credentials` file used by Amazon's aws command-line tool.
It can be created using the `aws` command:
```
$ aws configure
```
To configure a different profile, use the `--profile` flag
```
$ aws configure --profile other_profile
```

The `~/.aws/credentials` file can also be populated manually:
```
[default]
aws_access_key_id = ACCESS_KEY_ID_HERE
aws_secret_access_key = SECRET_ACCESS_KEY_HERE
```

To install the `aws` command in the SDK, run:
```
sudo emerge --ask awscli
```

### azure
`azure` uses `~/.azure/azureProfile.json`. This can be created using the `az` [command](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli):
```
$ az login`
```
It also requires that the environment variable `AZURE_AUTH_LOCATION` points to a JSON file (this can also be set via the `--azure-auth` parameter). The JSON file will require a service provider active directory account to be created.

Service provider accounts can be created via the `az` command (the output will contain an `appId` field which is used as the `clientId` variable in the `AZURE_AUTH_LOCATION` JSON):
```
az ad sp create-for-rbac
```

The client secret can be created inside of the Azure portal when looking at the service provider account under the `Azure Active Directory` service on the `App registrations` tab.

You can find your subscriptionId & tenantId in the `~/.azure/azureProfile.json` via:
```
cat ~/.azure/azureProfile.json | jq '{subscriptionId: .subscriptions[].id, tenantId: .subscriptions[].tenantId}'
```

The JSON file exported to the variable `AZURE_AUTH_LOCATION` should be generated by hand and have the following contents:
```
{
  "clientId": "<service provider id>", 
  "clientSecret": "<service provider secret>", 
  "subscriptionId": "<subscription id>", 
  "tenantId": "<tenant id>", 
  "activeDirectoryEndpointUrl": "https://login.microsoftonline.com", 
  "resourceManagerEndpointUrl": "https://management.azure.com/", 
  "activeDirectoryGraphResourceId": "https://graph.windows.net/", 
  "sqlManagementEndpointUrl": "https://management.core.windows.net:8443/", 
  "galleryEndpointUrl": "https://gallery.azure.com/", 
  "managementEndpointUrl": "https://management.core.windows.net/"
}

```

### do
`do` uses `~/.config/digitalocean.json`. This can be configured manually:
```
{
    "default": {
        "token": "token goes here"
    }
}
```

### esx
`esx` uses `~/.config/esx.json`. This can be configured manually:
```
{
    "default": {
        "server": "server.address.goes.here",
        "user": "user.goes.here",
        "password": "password.goes.here"
    }
}
```

### gcp
`gcp` uses `~/.config/gcp.json`, which contains a JSON-formatted service
account key. This can be downloaded from the Google Cloud console under
IAM > Service Accounts > [account] > Keys.

### openstack
`openstack` uses `~/.config/openstack.json`. This can be configured manually:
```
{
    "default": {
        "auth_url": "auth url here",
        "tenant_id": "tenant id here",
        "tenant_name": "tenant name here",
        "username": "username here",
        "password": "password here",
        "user_domain": "domain id here",
        "floating_ip_pool": "floating ip pool here",
        "region_name": "region here"
    }
}
```

`user_domain` is required on some newer versions of OpenStack using Keystone V3 but is optional on older versions. `floating_ip_pool` and `region_name` can be optionally specified here to be used as a default if not specified on the command line.

### packet
`packet` uses `~/.config/packet.json`. This can be configured manually:
```
{
	"default": {
		"api_key": "your api key here",
		"project": "project id here"
	}
}
```

### ibmcloud
`ibmcloud` uses `~/.bluemix/apikey.json`. This can be populated by downloading the apikey from the IBMCloud UI (https://cloud.ibm.com/login) or by using the IBMCloud cli (https://cloud.ibm.com/docs/cli?topic=cli-install-ibmcloud-cli). This would require the user to login with the correct credentials:
```
$ ibmcloud login --sso
```
or by using an existing apikey:
```
$ ibmcloud login --apikey <user-api-key>
```
Once logged in, an api key can be created:
```
 $ ibmcloud iam api-key-create other_key --file ~/.bluemix/apikey.json --output json
```

The json file should have the following fields at the minimum with the api key being mandatory:
```
{
	"name": "api key name here",
	"description": "description of api key usage here",
	"createdAt": "timestamp of creation here (UTC)",
	"apikey": "api key here"
}
```

### qemu
`qemu` is run locally and needs no credentials. It has a few restrictions:

- No [Local cluster](platform/local/)
- Usermode networking (no namespaced networks):
  * Single node only, no machine to machine networking
  * Machines have internet access by default
