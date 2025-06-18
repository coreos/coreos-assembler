# Hermetic builds for coreos-assembler and Konflux

The `*.lock.yaml` files generated will be consumed by the [prefetch-dependencies-oci-ta](https://github.com/konflux-ci/build-definitions/tree/main/task/prefetch-dependencies-oci-ta) Konflux task.
This task will download the dependencies and generate an OCI image containing them.
Then the OCI image will be pull during the build process by the [buildah-remote-oci-ta ](https://github.com/konflux-ci/build-definitions/tree/main/task/buildah-remote-oci-ta) Konflux task.

## To generate the rpms.lock.yaml file
The script below 1. updates the packages list in 'rpms.in.yaml' and 2. updates the 'rpms.lock.yaml' afterward.
The packages list is generated based on the content of the *deps*.txt file located in src/.
```bash
./update_rpms_lockfile
```
To test if everything is fine, you can fetch the dependencies and store them on your disk:
```bash
alias hermeto='podman run --rm -ti -v "$PWD:$PWD:z" -w "$PWD" quay.io/konflux-ci/hermeto:latest'
hermeto fetch-deps --dev-package-managers --source ./ --output ./hermeto-output '{"path": ".", "type": "rpm"}'
```
Konflux runs similar command within [prefetch-dependencies-oci-ta](https://github.com/konflux-ci/build-definitions/tree/main/task/prefetch-dependencies-oci-ta) task.

## To generate the artifacts.lock.yaml file
```bash
./update_artifacts_lockfile
```
To test if everything is fine, you can fetch the dependencies and store them on your disk:
```bash
alias hermeto='podman run --rm -ti -v "$PWD:$PWD:z" -w "$PWD" quay.io/konflux-ci/hermeto:latest'
hermeto fetch-deps --source ./ --output ./hermeto-output '{"path": ".", "type": "generic"}'
```

## Download everything together
```bash
alias hermeto='podman run --rm -ti -v "$PWD:$PWD:z" -w "$PWD" quay.io/konflux-ci/hermeto:latest'
hermeto fetch-deps --dev-package-managers --source ./ --output ./hermeto-output '[{"path": ".", "type": "rpm"}, {"path": ".", "type": "generic"}]'
```
