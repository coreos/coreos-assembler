# Adding/Updating kola Tests in coreos-assembler

For adding tests to `kola` for use in `coreos-assembler` please see the [kola test quickstart](https://github.com/coreos/mantle/blob/master/kola/README.md). After adding/updating tests in `kola` follow the `Updating Mantle` section in this document to pull in your new or updated tests into `coreos-assembler`.


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

# Running Unit Tests

1. Ensure that `pytest` and `pytest-cov` are installed:

```
$ pip3 install --user -r test-requirements.txt
```

2. Run `pytest` on the `tests` directory

```
$ pytest tests/
============================= test session starts ==============================
platform linux -- Python 3.7.3, pytest-4.6.3, py-1.8.0, pluggy-0.12.0
rootdir: /var/home/steve/Tech/GITHUB/coreos-assembler, inifile: pytest.ini
plugins: cov-2.7.1
collected 3 items

tests/test_cli.py ...                                                    [100%]

----------- coverage: platform linux, python 3.7.3-final-0 -----------
Name                      Stmts   Miss  Cover
---------------------------------------------
src/cosalib/__init__.py       0      0   100%
src/cosalib/build.py        127    127     0%
src/cosalib/cli.py           28      0   100%
---------------------------------------------
TOTAL                       155    127    18%


=========================== 3 passed in 0.05 seconds ===========================
```

# Using overrides

Development speed is closely tied to the "edit-compile-debug" cycle.  coreos-assembler
supports an `overrides/` directory to easily overlay locally-generated content
on top of the base OS content.

There are two subdirectories of `overrides/`:

- `overrides/rootfs`
- `overrides/rpm`

Let's say you want to hack on both ostree and ignition-dracut.  See
for example [this PR](https://github.com/coreos/ignition-dracut/pull/106)
which added support for `make install DESTDIR=` to the latter.  In general
most upstream build systems support something like this; if they don't
it's a good idea to add.

Concretely, after doing edits in a project, run a command like this:

`$ make install DESTDIR=/path/to/cosa-buildroot/overrides/rootfs`

This would then install files like
`/path/to/cosa-buildroot/overrides/usr/bin/ostree`
etc.

If you then run `cosa build`, those overrides will be automatically
incorporated.

You can also choose to use `overrides/rpm` - this accepts pre-built
binary RPMs.  If any RPMs are present here, then coreos-assembler
will automatically run `createrepo_c` and ensure that they are used
in the build.
