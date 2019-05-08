# Updating Mantle

Mantle houses a number of tools used within `coreos-assembler`. As an example, `kola` is part of mantle. Because of this it's required that `kola` tests are added in the upstream `kola` repo first, then synced into `coreos-assembler`. For more information on what tools are used please see the [README.md](README.md).

To update the `mantle` checkout within `coreos-assembler` the following steps must be done:

1. Update the `mantle/` checkout in the `coreos-assembler` repo to the version you expect
2. Add and commit the `mantle/` directory
3. Update the local submodule
4. PR your result

Here is an example for updating `mantle` to the latest code from it's own `master`:
```
$ pushd mantle
<snip/>
$ git pull origin master
<snip/>
$ popd
<snip/>
$ git commit -m "mantle: bump to current master (e7ab794)" mantle/
[ok 9236366] mantle: bump to current master (e7ab794)
 1 file changed, 1 insertion(+), 1 deletion(-)
$ git submodule update -- mantle
# Verify it's what you expect
$ cat .git/modules/mantle/HEAD
e7ab794c28cfd5d9d65ec34245aceaff92281be2
$ git push origin $YOURBRANCH
# Open PR
```

You can test the results by doing a build. See `Building the cosa container image locally` in [README.md](README.md#building-the-cosa-container-image-locally)