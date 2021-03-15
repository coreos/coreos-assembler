# Using Gangplank

Ganplank sole purpouse in life is to codify the knowledge of running building CoreOS variants and CoreOS-like operating systems using CoreOS Assembler. Gangplank knows how to create the environment for running, corrdinate artifacts from, and how to execute CoreOS Assembler.

## Terms

- OpenShift: Red Hat's Kubernetes Platform
- BuildConfig: a (Custom Resource Definition) CRD used by OpenShift that builds containers and other build artifacts
- Unbound Pod: Any instance of Gangplank that is running outside the context of an OpenShift BuildConfig

## Design Idea

Gangplank's core design principle is that containers are the most suitable modern method of orchestrating builds. Gangplank grew out of the various Jenkins libraries and scripts codifying the execution of various versions of COSA. 

Ganplank only knows _how to run COSA_, but running COSA does not require Gangplank. Today it understands how to:

- Run on generic Kubernetes version 1.17+ and OpenShift version 3.11 and 4.x. as an "unbound pod"
- Be used as an OpenShift BuildConfig
- Execute locally using podman
- Read meta.json and Jobspec files

### Menu vs Buffet

Gangplank, with the exception of local podman mode, is intended to run as in the CoreOS Assembler container. Prior to Gangaplnk, a considerable amount of time was spend keeping pipelines, JobSpecs and COSA code-bases in sync. Gangplank seeks to eliminate the mismatch by being part of CoreOS Assembler. Once started, Gangplank will be re-executed as a new pod that has suitable permissions and resources to build a CoreOS variant. When running on OpenShift or Kubernetes, Gangplank will use its pod specification to create worker pods. In other words, Gangplank is tightly coupled to its corresponding COSA container.

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

Gangplank has three execution modes each targeted at a different use-case. 

### OpenShift BuildConfig

Gangplank originally started as an OpenShift BuildConfig custom-build strategy. As a result, Gangplank uses the OpenShift BuildAPI v1 object definition for performing builds. When run as a BuildConfig, Gangplank can perform builds via `oc start-build`. 

The BuildConfig mode is intended for developer and re-build tasks. 

### Unbounded Pods on OpenShift or Kubernetes

Gangplank will execute happily on a generic Kubernetes or OpenShift 3.11 (requirement for an SCC privileged account, worker nodes must have a `/dev/kvm`) or OpenShift 4.5+ (must have access to the kube-virt labeled nodes)

This mode of operation is called "unbounded" since the pod is not bound to a BuildConfig, and something else (such as CI) is corrdinating the pod's life-cycle. 

Unbounded mode is targeted at Jenkins and other CI build systems. 

### Podman mode (for Developers)

For the developer use-case or even building on virtual machine, Gangplank supports running as a `podman` privileged pod. In podman, Ganplank will create worker pods. 

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

#### Secret Discovery

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

### Workers and their Work

The difference between an origin pod and worker is determinted by a single environment variable. If an envVar of `COSA_WORK_POD_JSON` is defined, then Gangplank will execute as a worker; if the OpenShift Build API envVar of `BUILD` is defined, then Gangplank will attempt to be an origin pod.

At start, Gangplank will decode the envVar of `COSA_WORK_POD_JSON`, which is defined by the origin pod when constructing the pod definition of the worker. When the origin pod is ready to start work, a Minio instance will be started using a random access/secret keys that will be added to the `COSA_WORK_POD_JSON`. 

Once the required artifacts, if any, are found, Gangplank will then start the worker pod. The worker pod will always run `cosa init` before running any other command. Then, the worker pod, will request dependencies over Minio from the orgin Gangplank, process the work, and then return _known_ files back 

Regardless of where the pod is being run, Gangplank will stream logs from the worker pods. If the supervising Gangplank is terminated, the workers are terminated.

When `builds.json` and `meta.json` are found, Gangplank will always transmit these files to the workers. `kola` test results, logs and all `meta.json` files are always returned to the origin Gangplank.

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

The JobSpec (or Job Specification) is simply YAML that instruct Gangplank on the steps and dependencies for starting a build.

To get started with a JobSpec, you can generate one using Gangplank.:
```
$ bin/gangplank generate -A base
INFO[0000] Gangplank: COSA OpenShift job runner, 2021-03-02.9dce8136~dirty
# Generated by Gangplank CLI
# 2021-03-02T17:25:42-07:00
job:
  strict: true
recipe:
  git_ref: testing-devel
  git_url: https://github.com/coreos/fedora-coreos-config
stages:
- id: Generated Base Stage
  build_artifacts: [base]
- id: Generated Finalize Stage
  build_artifacts: [finalize]
delay_meta_merge: true
```

The JobSpec defines discrete, units of work as a "stage". Each stage supports few options:

- id: the name of the stage; must be unique
- command: a list of commands to execute
- concurrent_execution: bool to indicate if the `commands` can be executed concurrently
- build_artifacts: known artifacts to build
- direct_execution: do not apply templating
- prep_commands: a list of commands to run before command
- post_commands: a list of commands to run last (such as test or cleanup)
- post_always: a bool that indicates whether the `post_commands` should _always_ be executed regardless of the success of the `commands` stage.
- require_artifacts: the type of artifact that's required for work (i.e. `qemu` or `aws`). Stages will not start until the artifact appears.

To illustrate this, consider:
```yaml
- id: base Stage
  build_artifacts:
  - base
  post_commands:
  - cosa kola run --basic-qemu-scenarios
- id: clouds
  concurrent_execution: true
  build_artifacts:
  - aws
  - gcp
  required_artifacts:
  - qemu
- id: finalize
  build_artifacts:
  - finalize
```

In this example, Gangplank will:

1. Build and test base artifacts in the first pod
1. In a new pod, concurrently build AWS and GCP but only after the QEMU artifact is found.
1. Combine meta.json together

### Meta Data and JobSpec Templating

Gangplank was initially started after belately realizing that the Jenkins Pipelines are, in fact, complicated templating engines. That is, a considerable amount of time, energy and development was put into translating data from YAML/JSON into execution rules.

Gangplank supports rendering commands from the `meta.json` in CoreOS Assembler and the JobSpec via Golang templates. The caveat, however, is that `meta.json` variables appears _after_ the base build. Generally speaking, this means to a base build are defined in the Jobspec while artifacts generated from a base build may use both `meta.json` and the Jobspec. 

#### JobSpec Example

Any JobSpec variable is exposed using the GoLang templating `{{.JobSpec.<VAR>}}`

```
archives:
  s3:
    bucket: darkarts
    path: magicalmysteries
stages:
  - id: prep
    command:
    - cosa buildprep s3://{{.JobSpec.Archives.S3.Bucket}}/{{.JobSpec.Archives.S3.Path}}
```

The above example will run the CoreOS Assembler command `cosa buildprep s3://darkarts/magicalmysteries`. 

#### meta.json

`meta.json` fields become available for any stage that is executed after Gangplank detects a new `meta.json`. Data for a `meta.json` is prefixed using `.Meta`. `meta.json` is always read immediately before a stage is executed (if `meta.json` exists). 
 
```
stages:
 - id: build
   build_artifacts:
   - base
 - id: make a file
   command:
   - touch {{ .Meta.BuildID }}
```

### Templating Logic

With the availability of GoLang templating, the possibility exists to do loops and to dynamically create commands. The following example, publishes an AMI to all AWS regions. 

NOTE: It may be tempting to turn Ganglank into a complicated templating engine. Users would well be advised to consider whether the added complexity helps. In most cases, using simple, clear, and easy to understand templating logic is the better choice.

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

### The Schema

CoreOS Assembler and Mantle (publication and testing for CoreOS-like operating sytems) share a schema that understands `meta.json`. Gangplank only understands a few commands does understand the location of artifacts. When artifacts are added to, or removed from, the [CoreOS Assembler schema](../../src/schema/v1.json) Gangplank's support will change.

Gangplank used the schema for:

- locating artifacts via their top level name (i.e. `qemu` or `metal4k`)
- creating `cosa buildextend-*` commands
- templating commands
