# plume

CoreOS release utility

## Testing

### Build a release image with the SDK

```sh
# Use same build ID for all boards
export COREOS_BUILD_ID=$(date +%Y-%m-%d-%H%M)
KEYID="<keyid>"
gpg2 --armor --export "$KEYID" > ~/keyfile
for board in amd64-usr arm64-usr; do
    ./build_packages --board=$board
    ./build_image --board=$board --upload --sign="$KEYID" prod
done
# amd64-usr only
for format in ami_vmdk azure gce; do
    ./image_to_vm.sh --prod_image --board=amd64-usr --format=$format --upload --sign="$KEYID"
done
```

### Perform the "release"

```sh
for board in amd64-usr arm64-usr; do
    bin/plume pre-release -C dev --verify-key ~/keyfile -B $board -V $version-$COREOS_BUILD_ID
done
for board in amd64-usr arm64-usr; do
    bin/plume release -C dev -B $board -V <version>-$COREOS_BUILD_ID
done
```

### Clean up

Delete:

- Stuff uploaded into `gs://users.developer.core-os.net/$USER`
- GCE image in `coreos-gce-testing`
- Azure image
- Azure blobs in `plume-devel` and `plume-devel-copy`
- AWS AMIs and snapshots in `us-west-1`, `us-west-2`, and `us-east-2`
