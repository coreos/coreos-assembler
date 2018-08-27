# plume

CoreOS release utility

## Testing

### Build a release image with the SDK

```sh
export COREOS_BUILD_ID=$(date +%Y-%m-%d-%H%M)
KEYID="<keyid>"
gpg2 --armor --export "$KEYID" > ~/keyfile
./build_packages
./build_image --upload --sign="$KEYID" prod
for format in ami_vmdk azure gce; do
    ./image_to_vm.sh --format=$format --upload --sign="$KEYID"
done
```

### Perform the "release"

```sh
bin/plume pre-release -C user --verify-key ~/keyfile -V $version-$COREOS_BUILD_ID
bin/plume release -C user -V <version>-$COREOS_BUILD_ID
```

### Clean up

Delete:

- Stuff uploaded into `gs://users.developer.core-os.net/$USER`
- GCE image in `coreos-gce-testing`
- AWS AMIs and snapshots in `us-west-1`, `us-west-2`, and `us-east-2`
