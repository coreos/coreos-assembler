# WHY Gangplank?

Arr matey....

Introduced as [part of PR 1739](https://github.com/coreos/coreos-assembler/pull/1739), the GoLang Gangplank is a CI-specific alternative "entrypoint" for executing CoreOS Assembler (COSA).

## Jenkins Pipelines are Greedy

One of the lessons learned from the RHCOS pipelines is that they are expensive in terms of resources. Only a few steps (build and KOLA) actually require KVM access, and then most commands do not require 4Gb of RAM. However, the entire pipeline run from start to finish, needs to run with resource guarantees of the most resource intensive step. To whit:
- Base builds need ~5Gb of disk space for cache, 4Gb of RAM and KVM access
- KOLA testing requires 1-2Gb of RAM per thread
- When building all the artifacts, 60Gb of ephemeral storage is needed before compression. On over-subscribed nodes, we have had to play games with PVC's in order to avoid node evictions for consuming too much disk space.
- The OCP cluster for the RHCOS pipeline only has a few nodes that are capable of running the pipeline. Development, developer and production-delivery pipelines all compete for the same nodes. This has led to pipeline-launched Pods, being evicted during later stages.
- Environmental differences between the COSA CI, FCOS and RHCOS pipelines has resulted in considerable drift.

Running multiple Jenkins pods is one way to deal with this. Yet, each Jenkins launched pod requires both a COSA and Agent container. In the RHCOS case, we actually have to run three containers (COSA, Jenkins and the Message Bus container) -- adding an extra 512Mb to each pod, all scheduled on an over-subscribed resource. Nevermind the maintance cost of Jenkins.

The other problem with Jenkins is that you _need_:
- The COSA Image name. The RHCOS pipeline has to know what version of COSA to use since there is a different version for each RHCOS release.
- An Agent and Master image for Jenkins itself.
- A pantheon of Cloud publication secrets that either are stored in Jenkins or in OCP itself. When the RHCOS pipeline started using OpenShift secrets, we ended up mounting roughly a dozen `volumeMounts` to each pod.
- The agents often timeout, and perform poorly over high latency connections.

While it's possible to run cross-cluster with Jenkins, in reality, it almost is never done. In fact, for the RHCOS pipelines, we have three teams running various versions of Jenkins and pipeline editions. The nicities of Jenkins in this world, are in fact, liabilities. A common theme for the production delivery team is various Jenkins issues. Worse, for each architecture of RHCOS we have have to stand up Jenkins and then populate an OCP namespace.

## Jenkins has become a templating engine

In the RHCOS case, a considerable amount of Groovy has been written to parse, check and emit "cosa <TARGET> <CLI ARGS>" commands. The FCOS pipeline is easier to maintain and understand; the RHCOS pipeline is a special snowflake of variable-controlled rules. The complexity of RHCOS's pipeline comes from the business rules requiring special logic. FCOS's pipeline only has to support a single configuration, while RHCOS has to support at least three releases on four architectures.

Initially the RCHOS pipeline uses OpenShift BuildConfig with envVars. Over time, almost all of these envVars and even Jenkins job parameters were removed. As it turns out, converting YAML to envVars to Groovy to Shell is ripe for type errors; this was especially true when dealing with truthiness.

To help deal with truthiness, the RHCOS pipeline grew the "JobSpec" (as in Jenkins Job). The JobSpec was an attempt at creating a declarative method of setting variables for the pipeline to consume. This idea allows the RHCOS pipeline to run entirely via GitOps and without having to deal with type-conversion errors, or having envVars from a BuildConfig, to provide for dynamic behavior.

## The Cost of Developing Jenkins

The problem with using Jenkins as a templating engine is that it is incredibly inefficient for testing. Consider:
1. changes have to be commited first
1. then a build has to be started
1. a human has to check the run
1. repeat, repeat....

The problem with this model is:
- it requires developers to invest in Jenkins and, by extension Groovy
- it's wasteful in terms of resources and re-enforces git-smash'ng
- there is no way to have a pure logic-check run -- the commands have to actually be run

Some work has been done to CI the CI, which introduced its own class of problems.

## The Problem

While the JobSpec provided a means of GitOps controlled execution of the Jenkins Job, *it was simply wall-papering over a glaring missing feature in COSA: the lack of a non-CLI interface into COSA*. A review of the three COSA CI pipelines shows that Jenkins provides:
- launching pods
- templating COSA commands
- a pretty web-UI

In fact, the problem is a bit deeper:
- COSA assumes an order of operations that is codified in the code, but not documented. The Jenkins pipelines are quasi-authoritative in the order of operations. To whit: `cosa build` must preceed a `cosa buildextend`, some artifacts require the `metal*` artifacts while others require just the `qcow2`.
- The CLI interface is inconsistent. Some commands are Bash, others Python and use different styled arguments.
- The notion of `build` is based on positional perception: COSA considers building the OSTree a build, but by default it builds the `qcow2`. Pipelines consider creating `artifacts` as "building". And users consider a "build" to be _all_ the produced artifacts.
- When COSA changes, all the pipelines have to change.

## The Solution

Gangplank is proposed as the "thing" to provide stable interface(s) into COSA. [Previously an envVar interface](https://github.com/coreos/enhancements/pull/1) was proposed. Bluntly speaking, the idea was not well-received. Gangplank seeks to provide a set of interfaces into COSA that:
- provide a means of file-based instructions to COSA
- provide a means of templating the COSA commands
- initially provide the RHCOS JobSpec to templated COSA commands
- act as a CI `ENTRYPOINT` for COSA containers built to run in OCP
- run COSA as a first-class OpenShift Custom Builder
- provide testable code for parsing the commands
- avoid migrating Jenkins pipeline to Tekton

While `gangplank` currently supports the RHCOS JobSpec, it is anticipated that other "specifications" will be introduced such as OCP's BuildConfig Specification.

## GoLang to the rescue

The bulk of COSA code is either Bash or Python. [It has been proposed that we support commands in GoLang](https://github.com/coreos/coreos-assembler/issues/1668), previously. And since COSA swallowed Mantle, there is a third language: GoLang.

GoLang was chosen over Bash or Python for several reasons:
- GoLang is a compiled language. For something acting as an orchestrator, run-time compilation or scripts are too fragile.
- GoLang is strictly typed. Going from strictly typed to the loosely typed Bash or Python is "safer".
- The contributing developers of COSA prefer Bash or GoLang over Python.
- GoLang templating is commonly used in the OpenShift program.
- Bash is largely untestable.
- GoLang avoids previous COSA disputes regarding OOO and style.

## Why not OpenShift Templates?

An early lesson learned writing the RHCOS pipeline is that while an OpenShift template is trivial, they tend to pollute the namespace. OpenShift templates are great for deploying an application, but become tedious when deploying arbitrary configurations. For example, using an OpenShift template to deploy test, dev, and production configurations could require three separate deployments when all that changes is a single variable.

The vision of the `gangplank` is to create a templated execution of COSA based on the file interface. That is, instead of managing different deployments, COSA will take a configuration (the JobSpec) and `run-steps`. A single `buildconfig` can service the needs of developers and production environments.

## Jenkins as a helper

Jenkins is NOT going away in this world view. Rather, Jenkins will not be directly scheduling the pods. A new set of COSA CI Libs will be created that provide wrappers around the `oc` binary for calling OpenShift BuildConfig.

An example invocation might look like:
```
       stage("build") {
           parallel x86: {
               cosa_oc("creds-x86", "build.steps")
           } aarch64: {
               cosa_oc("creds-aarch64", "build.steps")
           }
```

Where `cosa_oc` is the wrapper that:
- sets `KUBECONFIG=creds-*`
- creates a `build.tar` containing the `JobSpec`, `build.steps`
- calls `oc start-build bc/cosa-priv --from-archive=build.tar --follow=true`

In this world, the Secrets would exist outside of Jenkins and would be stored in the OpenShift environment and referenced in the `buildConfig` itself. Gangplank supports the OpenShift BuildAPI and the Kubernetes APIs.
- unpack `build.tar`
- find the `jobspec` and the `build.steps`
- execute the steps

Since the builds are using `buildConfigs`, each "build" is repeatable.

Ideally, there would be BuildConfigs for:
- privileged execution for builds that need direct /dev/kvm access
- privileged execution for testing
- unprivileged execution publication steps

## Development and Pipeline Parity

A profound pain point for COSA _and_ pipeline development is that environmental differences between the developer (and, by extension their pet container), and COSA, FCOS and RHCOS pipelines can cause a rousing round of "fix a bug whack-a-mole" (where the code works in one pipeline, but not another). `entrypoint` seeks to solve that by removing Jenkins from the Pod execution by allowing the developer to run pipeline code locally. That is, a developer should have reasonable assurances that if they locally run steps via `podman -it --entrypoint /usr/bin/gangplank coreos-assembler....` it will succeed in one of the pipelines.

## `cosa remote`

In the "Jenkins as a Helper" section, a curious opening appears -- the ability to run `cosa` commands _remotely_ in an OpenShift Cluster.

For those unlucky enough to obtain their internet access from a major US-based cable monopoly, an incredible pain point is the "build-upload" cycle:
1. developer begs around for $CLOUD credentials
1. they hack on COSA, RPM's, overrides, etc.
1. build
1. upload
1. do something else while 20G image is slurped up at 250Kbs...
1. repeats steps 2-5

By having COSA as a `buildConfig`, we can now have a `cosa remote` command that:
- creates a `devel.tar` of `src`, `overrides`, and local COSA hacks with a JobSpec and `build.steps`
- call `oc start-build bc/cosa-priv --from-archive=devel.tar --env=DEVELOPER_MODE=1 --follow=true`

When the buildConfig starts, it would upack `devel.tar` and then exec into the developer's local COSA environment running remotely. This would save the developer from:
1. having to get their own credentials
1. the build happens close to the source
1. when pushing the build, the developer's in-house broadband is not used
1. development time can be significantly reduced.

## In Conclusion

The rationale behind draining the pipelines into Jenkins is a question of developer efficiency, satisfaction, and reducing the operational burden.
