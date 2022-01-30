# Using Gangplank

Gangplank's sole purpose in life is to codify the knowledge of building CoreOS variants and CoreOS-like operating systems using CoreOS Assembler. Gangplank knows how to create the environment, execute CoreOS Assembler, and coordinate artifacts.

## Terms

- OpenShift: Red Hat's Kubernetes Platform
- BuildConfig: a (Custom Resource Definition) CRD used by OpenShift that builds containers and other build artifacts
- Unbound Pod: Any instance of Gangplank that is running outside the context of an OpenShift BuildConfig

## Design Idea

Gangplank's core design principle is that containers are the most suitable modern method of orchestrating builds. Gangplank grew out of the various Jenkins libraries and scripts codifying the execution of various versions of COSA.

Gangplank only knows _how to run COSA_, but running COSA does not require Gangplank. Today it understands how to:

- Run on generic Kubernetes version 1.17+ and OpenShift version 3.11 and 4.x. as an "unbound pod"
- Be used as an OpenShift BuildConfig
- Execute locally using podman
- Read meta.json and Jobspec files

### Menu vs Buffet

Gangplank, with the exception of local podman mode, is intended to run in the CoreOS Assembler container. Prior to Gangaplnk, a considerable amount of time was spend keeping pipelines, JobSpecs and COSA code-bases in sync. Gangplank seeks to eliminate the mismatch by being part of CoreOS Assembler. Once started, Gangplank will be re-executed as a new pod that has suitable permissions and resources to build a CoreOS variant. When running on OpenShift or Kubernetes, Gangplank will use its pod specification to create worker pods. In other words, Gangplank is tightly coupled to its corresponding COSA container.

The origin pod (the first instance of Gangplank) handles the orchestration of workers by:
- parsing the environment
- reading the jobspec
- creating child worker pod definitions
- sending work to worker pods and waiting for completion of work
- life-cycle operations (create/delete/etc) for workers

Previous build systems have used Jenkins Kubernetes plugins for the pod creation and life-cycling of the worker pods. The problem with approach is that each OpenShift/Kubernetes environment would have unique differences that caused pipeline drift. For example, the production pipeline for RHCOS uses a different set of secret names than the development location.

Gangplank, therefore, evaulates its environment to determine the mode of the build.

*NOTE: When running in a Kubernetes/OpenShift cluster, Gangplank requires a service account that can read secrets AND create/delete pods.*

## Execution Choices

Gangplank has three execution modes, each targeted at a different use-case.

### OpenShift BuildConfig

Gangplank originally started as an OpenShift BuildConfig custom-build strategy. As a result, Gangplank uses the OpenShift BuildAPI v1 object definition for performing builds. When run as a BuildConfig, Gangplank can perform builds via `oc start-build`.

The BuildConfig mode is intended for developer and re-build tasks.

### Unbounded Pods on OpenShift or Kubernetes

Gangplank will execute happily on a generic Kubernetes or OpenShift 3.11 (requirement for an SCC privileged account, worker nodes must have a `/dev/kvm`) or OpenShift 4.5+ (must have access to the kube-virt labeled nodes)

This mode of operation is called "unbounded" since the pod is not bound to a BuildConfig, and something else (such as CI) is corrdinating the pod's life-cycle.

Unbounded mode is targeted at Jenkins and other CI build systems.

### Podman mode (for Developers)

For the developer use-case or even building on virtual machine, Gangplank supports running as a `podman` privileged pod. In podman, Gangplank will create worker pods.

This requires the `podman-remote` package installed and enabled (enabled, via `systemctl --now enable podman.socket`).

Example command:
```
$ gangplank pod
  --podman \
  --workDir <dir>
  --spec <jobspec>
```

The following are optional commands:
- `-A <artifact>` or `--artifact <artifact>` will build a specific artifact. When `-A` is provided, no jobspec is required.
- `--srvDir` directly expose `/srv` to the pod. If this is not defined, then Gangplank will create an ephemeral working `/srv` which will be cleaned up on exit
- `setWorkDirCtx` will set the proper selinux permmissions for `--workDir` and `--srvDir`

If `--workDir` is defined, the build output will be emited to `<workDir>/builds`.

*btrfs warning*: Gangplank can run multiple pods at a single time. When done on a `btrfs`, the clean-up can be are ridiciously slow/hang. If you are building on `btrfs` (default for Fedora Workstation 33+), it is recommended that you turn off copy-on-write (COW) and use a `--workDir` with that directory if using parallel stages. Example:
```
mkdir ~/workdir
chattr +C file ~/workdir
gangplank pod --workDir ~/workDir <options
```

### Remote Podman

Gangplank supports the use of remote Podman containers via the Podman GoLang bindings. Since the binding are controllable through envVars, Gangplank will blindly run remote containers in podman mode when `CONTAINER_HOST` is defined, although Gangplank tries to be smart about it.

To use remote podman, users are advised to [follow the remote-podman guide](https://github.com/containers/podman/blob/master/docs/tutorials/remote_client.md).

Example invocations:
```
$ gangplank pod --podman --remote ssh://tr@horcrux.dev/run/user/1000/podman/podman.sock -A base
```

or

```
$ export CONTAINER_HOST='ssh://tr@horcrux.dev/run/user/1000/podman/podman.sock'
$ export CONTAINER_SSHKEY='/path/to/sskkey'
$ gangplank pod --podman -A base
```

Unless an external Minio Server has been defined, Gangplank will forward Minio over SSH for return of the artifacts.

If running remote Podman to build a different architecture than where Gangplank is being called (ie. `x86_64` remoting to `aarch64`), the `-a` (`--arch`) flag must be specified. Omitting this will result in artifacts not being upload to Minio, whether using an external server or over SSH.

```sh
$ gangplank pod --podman -A base -a aarch64
```

#### Secret/Config Map Discovery (Kubernetes/OCP)
Gangplank has first-class secret discovery, but does not require them. To find secrets, Gangplank will iterate over all secrets that have been annotated using `coreos-assembler.coreos.com/secret` and check the value against known secrets (i.e. AWS, GCP, etc.). If the secret is known, then the _workers_ will have the secret exposed via envVar or with an envVar pointer to the files.

For example:
```yaml
       apiVersion: v1
        data:
          aws_default_region: dXMtZWFzdC0xCg==
          config:...
        kind: Secret
        metadata:
          annotations:
          labels:
            coreos-assembler.coreos.com/secret: aws
          name: my-super-secret-AWS-keys
        type: Opaque
```

Gangplank tries to follow the upstream tooling convention regarding secrets; if the most popular tool uses an envVar secret then Gangplank will too.

In the above example, Gangplank will expose `AWS_DEFAULT_REGION` to be `us-east-1` and set `AWS_CONFIG_FILE` to the in-pod location of config file.


Gangplank also uses Volume Mounts defined by Config Maps. Gangplank will look for known Config Maps (internal-ca, Push/Pull secrets, Koji secrets, etc) using the label `coreos-assembler.coreos.com/mount-ref` and mount them as defined.

For example:
```yaml
        apiVersion: v1
          data:
            config..
          kind: ConfigMap
          metadata:
            annotations:
            labels:
              coreos-assembler.coreos.com/mount-ref: krb5.conf
              type: config
            name: brew-krb-config
```
In this case Gangplank checks the configuration done for krbr.conf and  mounts it as define in [volumes.go](https://github.com/coreos/coreos-assembler/blob/327d2f4a2764b1a5cd5c2b5a01eff1421615c614/gangplank/internal/ocp/volumes.go#L118-L139).

The Push/Pull secrets are a different type of secret, and need a verification against the registry to be mounted as a docker config under the cosaDir.


### Workers and their Work

The difference between an origin pod and worker is determined by a single environment variable. If an envVar of `COSA_WORK_POD_JSON` is defined, then Gangplank will execute as a worker; if the OpenShift Build API envVar of `BUILD` is defined, then Gangplank will attempt to be an origin pod.

At start, Gangplank will decode the envVar `COSA_WORK_POD_JSON`, which is defined by the origin pod when constructing the pod definition of the worker. When the origin pod is ready to start work, a Minio instance will be started using a random access/secret keys that will be added to the `COSA_WORK_POD_JSON` envVar.

Once the required artifacts, if any, are found, Gangplank will then start the worker pod. The worker pod will always run `cosa init` before running any other command. Then, the worker pod will request dependencies over Minio from the origin Gangplank, process the work, and then return _known_ files back.

If you are running Gangplank via a CI/CD runner, and you want to visualize the stages better, Gangplank allows to use a shared or external minio instance. To use a shared instance, start a background instance of Gangplank via `(gangplank minio --minioSrvDir <path> -m minio.cfg`), then add `-m minio.cfg` to all other Gangplank commands. Gangplank further supports the use of S3-compatible object stores (i.e. AWS) via the `-m` directive. Gangplank uses the object store backend for reading files and discovery of requirements.

Regardless of where the pod is being run, Gangplank will stream logs from the worker pods. If the supervising Gangplank is terminated, the workers are terminated.

All meta-data that is found will be provided to the workers. `kola` test results, logs and new meta-data and any new artifact generated are returned to the origin Gangplank.

### Build Short-hands

To support distributed builds, Gangplank has two special build short-hands of "base" and "finalize":

```yaml
stages:
 - id: base
   build_artifacts:
   - base
   - finalize
```

The `base` short-hand corresponds to `cosa build --delay-meta-merge`, while `finalize` corresponds to `cosa meta --finalize`. By default, `cosa build` uses the `delay-meta-merge` since Gangplank is performing a distributed build. In general `finalize` should be the last stage.

### JobSpec

The JobSpec (or Job Specification) is simply YAML that instructs Gangplank on the steps and dependencies for starting a build.

To get started with a JobSpec, you can generate one using Gangplank via `gangplank generate -A base`

Example spec:
```
$ bin/gangplank generate -A base
INFO[0000] Gangplank: COSA OpenShift job runner, 2021-03-02.9dce8136~dirty
# Generated by Gangplank CLI
# 2021-03-02T17:25:42-07:00
job:
  strict: true

minio:
  bucket: builder

recipe:
  git_ref: "release-4.8"
  git_url: https://github.com/openshift/os
  repos:
   # Inline repos are defined in the jobspec
   - name: repos
     inline: |
        <INLINE DEFINITION>
   # URL should reference a file with repository definition(s)
   - name: repo1
     url: https://example.com/repo/repo.file

# publish_ocontainer describes locations to push the oscontainer to.
publish_oscontainer:
    # TLS verification for build strategy builds. Defaults to true
    # Push registry comes from the build.openshift.io's build.spec.output
    # specification.
    buildstrategy_tls_verify: true

    # list of push locations to push osconatiner to.
    registries:
      # push to a cluster address using an service account token
      # to login to the regitry (only useful in cluster)
      - url: "first.registry.example.com/stream/name:tag"
        secret_type: token
        tls_verify: false

      # push with an inline secret
      - url: "second.registry.example.com/stream/name:tag",
        secret_type: inline
        secret: "<STRING>"

      # push using an incluser secret name "builder-secret"
      # the service account running Gangplank will need to be
      # able to read the secret
      - url: "third.registry.exmaple.com/stream/name:tag",
        secret_type: cluster
        secret: builder-secret

- id: ExecOrder 1 Stage
  description: Stage 1 execution base
  build_artifacts: [base]
  execution_order: 1
  request_cache: true
  request_cache_repo: true

- id: ExecOrder 5 Stage
  description: Stage 5 execution aws
  require_artifacts: [qemu]
  build_artifacts: [aws]
  execution_order: 5

- id: ExecOrder 5 Stage
  description: Stage 5 execution gcp
  require_artifacts: [qemu]
  build_artifacts: [gcp]
  execution_order: 5

delay_meta_merge: true

```

The JobSpec defines discrete units of work as a "stage". Each stage supports few options:

- id: the name of the stage; must be unique
- command: a list of commands to execute
- concurrent: bool to indicate if the `commands` can be executed concurrently
- build_artifacts: known artifacts to build
- direct_execution: do not apply templating
- prep_commands: a list of commands to run before command
- post_commands: a list of commands to run last (such as test or cleanup)
- post_always: a bool that indicates whether the `post_commands` should _always_ be executed regardless of the success of the `commands` stage.
- require_artifacts: the type of artifact that's required for work (i.e. `qemu` or `aws`). Stages will not start until the artifact appears.
- request_artifacts: a list of optional artifacts that would be _nice_ to have, but are not blocking.
- {return,require,request}{cache,cache_repo}: bool value that indicates whether to requires, require or return a tarball of the cache (`/srv/cache`) or the cache_repo (`/srv/tmp/repo`).

To illustrate this, consider:
```yaml

- id: ExecOrder 1 Stage
  description: Stage 1 execution base
  build_artifacts: [base]
  execution_order: 1
  request_cache: true
  request_cache_repo: true

- id: ExecOrder 5 Stage
  description: Stage 5 execution aws
  require_artifacts: [qemu]
  build_artifacts: [aws]
  execution_order: 5

- id: ExecOrder 5 Stage
  description: Stage 5 execution gcp
  require_artifacts: [qemu]
  build_artifacts: [gcp]
  execution_order: 5

- id: ExecOrder 999 Stage
  description: Stage 999 execution finalize
  build_artifacts: [finalize]
  execution_order: 999

```

In this example:

1. In the base stage, Gangplank will provide `/srv/cache` and `/srv/tmp/repo` from `cache/*` if the tarballs exist, and optionally provide the latest `oscontainer`. Gangplank will return the build artifacts and new cache tarballs.
1. In the oscontainer stage, Gangplank will require the caches.
1. In the `ExecOrder 5` stages, two new pods will concurrently build AWS and GCP but only after the QEMU artifact is found.
1. The final `ExecOrder 999` stage will combine `meta.*.json` to produce a final `meta.json`.

### Meta Data and JobSpec Templating

Gangplank was initially started after belately realizing that the Jenkins Pipelines are, in fact, complicated templating engines. That is, a considerable amount of time, energy and development was put into translating data from YAML/JSON into execution rules.

Gangplank supports rendering commands from the `meta.json` in CoreOS Assembler and the JobSpec via Golang templates. The caveat, however, is that `meta.json` variables appear _after_ the base build. Generally speaking, this means inputs to a base build are defined in the Jobspec while artifacts generated from a base build may use both `meta.json` and the Jobspec.

#### JobSpec Example

{% raw %}
Any JobSpec variable is exposed using the GoLang templating `{{.JobSpec.<VAR>}}`

```
archives:
  s3:
    bucket: darkarts
    path: magicalmysteries
stages:
  - id: prep
    command:
    - cosa buildfetch --url=s3://{{.JobSpec.Archives.S3.Bucket}}/{{.JobSpec.Archives.S3.Path}}
```
{% endraw %}

The above example will run the CoreOS Assembler command `cosa buildfetch --url=s3://darkarts/magicalmysteries`.

#### meta.json

`meta.json` fields become available for any stage that is executed after Gangplank detects a new `meta.json`. Data for a `meta.json` is prefixed using `.Meta`. `meta.json` is always read immediately before a stage is executed (if `meta.json` exists).

{% raw %}
```
stages:
 - id: build
   build_artifacts:
   - base
 - id: make a file
   command:
   - touch {{ .Meta.BuildID }}
```
{% endraw %}

### Templating Logic

With the availability of GoLang templating, the possibility exists to do loops and to dynamically create commands. The following example publishes an AMI to all AWS regions.

NOTE: It may be tempting to turn Gangplank into a complicated templating engine. Users would well be advised to consider whether the added complexity helps. In most cases, using simple, clear, and easy-to-understand templating logic is the better choice.

{% raw %}
```
archives:
  s3:
    bucket: darkarts
    path: magicalmysteries
clouds_cfgs:
  aws:
    amipath: foobar
    regions:
     - us-east-1
     - us-west-1
stages:
 - id: publish to AWS
   commands:
   -  upload-ami --build {{.Meta.BuildID}} --region {{.JobSpec.CloudsCfgs.Aws.Regions[0]}} --bucket=s3://{{.JobSpec.Archives.S3.Bucket}}/{{.JobSpec.Archives.S3.Path}}
   - cosa aws-replicate --build {{.Meta.BuildID}} --regions {{for _, $y := range .JobsSpec.CloudsCfgs.Aws.Regions}}{{$y}}{{end.}}
```
{% endraw %}

### The Schema

CoreOS Assembler and Mantle (publication and testing for CoreOS-like operating sytems) share a schema that understands `meta.json`. Gangplank only understands a few commands regarding the location of artifacts. When artifacts are added to, or removed from, the [CoreOS Assembler schema](../../src/schema/v1.json) Gangplank's support will change.

Gangplank uses the schema for:

- locating artifacts via their top level name (i.e. `qemu` or `metal4k`)
- creating `cosa buildextend-*` commands
- templating commands

## Minio

The choice of Minio was deliberate: its an open source S3-comptabile object store that is light weight, and has GoLang bindings. The use of Minio in the case of Gangplank is purely for the coordination files. Gangplank requires either Minio or access to an S3 object store.

### Standalone mode

If an external Minio/S3 server is not defined, Gangplank runs Minio from the directory defined as `--srvDir`. A new directory of "builder" (or whatever bucket you've chosen) will be created under the `--srvDir` parameter.

### External mode

Running Minio in external mode is relatively easy:
- [Simple OpenShift Deployment](https://github.com/darkmuggle/minio-ocp)
- [Minio's Official Kubuernetes Documentation](https://docs.min.io/docs/deploy-minio-on-kubernetes.html)
- Podman:
```
$ podman volume create minio
$ podman create -p 9000 --name minio -v minio:/data \
    -e MINIO_ACCESS_KEY=key \
    -e MINIO_SECRET_ACCESS_KEY=key \
    docker.io/minio/minio:latest \
    server /data
$ podman start minio
```

Gangplank understands how to use an external minio host via `-m config.json`. Where `config.json` has the following format:
```
{
  "accesskey": "minioadmin",
  "secretkey": "minioadmin",
  "host": "192.168.3.9",
  "port": 9000,
  "external_server": true,
  "region": ""
}
```

Example of AWS Config:
```
{
  "accesskey": "<ACCESS KEY>",
  "secretkey": "<SECRET ACCESS KEY>",
  "host": "us-west-1.s3.amazonaws.com",
  "port": 443
  "external_server": true,
  "region": "us-west-1"
}
```
