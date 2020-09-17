---
layout: default
parent: Mantle
nav_order: 1
---

# ore

Ore provides a low-level interface for each cloud provider. It has commands
related to launching instances on a variety of platforms (gcloud, aliyun, aws,
azure, esx, and packet) within the latest SDK image. Ore mimics the underlying
api for each cloud provider closely, so the interface for each cloud provider
is different. See each providers `help` command for the available actions.

Note, when uploading to some cloud providers (e.g. gce) the image may need to be packaged
with a different --format (e.g. --format=gce) when running `image_to_vm.sh`
