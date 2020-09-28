# Entrypoint

Introduced as [part of PR 1739](https://github.com/coreos/coreos-assembler/pull/1739), the GoLang Entrypoint a CI-specific alternative "entrypoint" for executing CoreOS Assemlber (COSA).

## Jenkins Pipeline are Greedy

One of the lessons learned from the RHCOS pipelines is that they are expensive in terms of resources. Only a few steps (build, and KOLA) actually require KVM access. However, the entire pipeline run from start to finish with the needed guarantees of the most resource intensive step. To whit:
- base builds need ~5Gb of disk space for cache, 4Gb of RAM and KVM access
- KOLA testing requires 1-2Gb of RAM per thread
- when building all the artifacts, 60Gb of ephemeral storage is needed before compression
- the OCP cluster for the RHCOS pipeline only has a few nodes that are capable of running the pipeline. Development, developer and production-delivery pipelines all compete for the same nodes. This has led to pipeline-launched Pods, being evicted during later stages.
- Environmental differences between the COSA CI, FCOS and RHCOS pipelines has resulted in considerable drift.

Running multiple Jenkins pods is one way to deal with this. Yet, each Jenkins launched agent pod, requires both a COSA and Agent container. In the RHCOS case, we actually have to run three containers (COSA, Jenkins and the Message Bus container) -- adding an extra 512Mb to each pod, all scheduled on an over-subscribed resource.

The other problem with Jenkins' agents is that you _need_ to know:
- the COSA Image name
- the Agent Image name
- the agents often timeout, and preform poorly over high latency connections

While its possible to run cross-cluster with Jenkins, in reality, its almost never done. In fact, for the RHCOS pipleines, we have three teams running various versions of Jenkins and pipeline editions. The nicities of Jenkins in this world, are in fact, liabilities. A common theme for the production delivery team various Jenkins issues.

## Jenkins has become a templating engine

In the RHCOS case, the a considerable amount of Groovy has been written to parse, check and emit "cosa <TARGET> <CLI ARGS>" commands. The FCOS pipleine is considerably easier to maintain and understand, while the RHCOS is special snowflake of variables controled rules. RHCOS's pipeline comes from the business rules requiring special buisness logic.

Initially the RCHOS pipeline uses OpenShift BuildConfig with envVars. Over time, almost all of these envVars and even Jenkins job parameters have been removed. As it turns out, converting YAML to envVars to Groovy to Shell is ripe for type errors; this was especially true when dealing with truthiness.

To help deal with truthiness, the RHCOS pipeline grew the "JobSpec" (as in Jenkins Job). The JobSpec was an attempt at creating a declarative method of setting variables for the pipeline to consume. This idea allows the RHCOS pipeline to run entirely via GitOps and without having to deal with type-conversion errors, envVars from BuildConfig and provided for dynamic behavior.

## The Cost of Developing Jenkins

The problem with using Jenkins as a templating engine is that its is incredibly inefficient for testing. Consider:
1. changes have to be commited first
1. then a build has to be started
1. a human has to check the run
1. repeat, repeat....

The problem with this model is:
- it requires developers to invest in Jenkins and, by extension Groovy
- its wasteful in terms of resources and re-enforces git-smash'ng
- there is no way to have a pure logic-check run -- the commands have to actually be run

Some work has been done to CI the CI.

## The Problem

While the JobSpec provided a means of GitOps controlled execution of the Jenkins Job, *it was simply wall-papering over a glaring missing feature in COSA: the lack of a non-CLI interface into COSA*. A review of the three COSA CI pipelines shows that Jenkins provides:
- launching a pods
- templating COSA commands
- a pretty web-UI

In fact, the problem is a bit deeper:
- COSA assumes an order of operations. This order of operation is codified in the code, but is not documented. The Jenkins pipelines are quasi-authoritative in the order of operations. To whit: `cosa build` must preceed a `cosa buildextend`, some artifacts require the `metal*` artifacts while others require just the `qcow2`.
- The CLI interface is inconsient. Some commands are Bash, others Python and use different styled arguments.
- The notion of `build` is positional -- COSA considers building the OSTree a build, but by default it builds the `qcow2`. Pipelines consider creating `artifacts` as "building". And users consider a "build" to be _all_ the produced artifacts.
- When COSA changes, all the pipelines have to change.

## The Solution

The `entrypoint` is proposed as the "thing" to provide an interface into COSA. [Previously an envVar interface](https://github.com/coreos/enhancements/pull/1) was proposed. Bluntly speaking, the idea was not well-recieved. The `entrypoint` seeks to provide a set of interfaces into COSA that:
- provides a means of file-based instrutions to COSA
- provides a means of templating the COSA commands
- initially provide the RHCOS JobSpec to templated COSA commands
- act as a CI `ENTRYPOINT` for COSA containers built to run in OCP
- run COSA as a full-righted OpenShift Custom Builder
- provide testable code for parsing the commands

While `entrypoint` current supports the RHCOS JobSpec, it is anticipated that other "specficiations" will be introduced for FCOS.

## GoLang to the rescue

The bulk of COSA code is either Bash or Python. [It has been proposed that we support command in GoLang](https://github.com/coreos/coreos-assembler/issues/1668), previously. And since COSA swallowed Mantle, there is a third-language GoLang.

GoLang was chosen over Bash or Python several reasons:
- GoLang is a compiled language.
- GoLang is strictly typed. Going from strictly typed to the loosely typed Bash or Python is "safer".
- The contributing developers of COSA prefer Bash or GoLang over Python.
- GoLang templating is commonly used in the OpenShift program.
- Bash is largely untestable.
- GoLang avoids previous COSA disputes regarding OOO and style.

## Why not OpenShift Tempalates?

An early lesson learned when doing the RHCOS pipeline is that while an OpenShift template is trivial, they tend to pollute the namespace. OpenShift templates are great for deploying an application, but become tedious when deploying arbitrary configruations. For example, using an OpenShift template to deploy test, dev, and production configurations could require three seperate deployements when all that changes is a single variable.

The vision of the `entrypoint` is to create a templated execution of COSA based the file interface. That is, instead of managing different deployments, COSA will take a configuration (the JobSpec) and `run-steps`. A single `buildconfig` can service the needs of developers and production enviroments.

## Jenkins as a helper

Jenkins is NOT going away in this world view. Rather, Jenkins will not be directly scheduling the pods. A new set of COSA CI Libs will be created that provide wrappers around the `oc` binary for calling Openshift BuildConfig.

And example invocation might look like:
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

In this world, the Secrets would exist outside of Jenkins and would be in the `buildConfig` itself. Then `entrypoint`, which will support the OpenShift `buildConfig` spec will:
- unpack `build.tar`
- find the `jobspec` and the `build.steps`
- execute the steps

Since the builds are using `buildConfigs`, each "build" is repeatable.

Ideally, there would be BuildConfigs for:
- privileged execution for builds that need direct /dev/kvm access
- privileged execution for testing
- unprivileged execution publication steps

## Development and Pipeline Parity

A profound pain point for COSA _and_ pipeline development is that environmental differences between the developer (and their pet container), and COSA, FCOS and RHCOS pipeline can cause a rousing round of "fix a bug whack-a-mole." (Where the code works in one pipeline, but not another) `entrpyoint` seeks to solve that removing Jenkins from the Pod execution by allowing the developer to run pipeline code locally. That is, a developer should have reasonable assurances that if they run locally run steps via `podman -it --entrypoint /usr/bin/entry coreos-assembler....` should succeed as if run in a pipeline.

## `cosa remote`

In the "Jenkins as a Helper" section, a curious opening appears -- the ability to run `cosa` commands _remotely_ in an OpenShift Cluster.

For those unlucky enough to be on major US-based cable monopolies, an incredible pain point is the "build-upload" cylce:
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
