Wraps [rpm-ostree compose tree](https://github.com/projectatomic/rpm-ostree/blob/master/docs/manual/compose-server.md) a
container.

Today, it mostly implements a YAML → JSON conversion for the treefiles, as I view
the use of JSON as a mistake for various reasons.

Usage example:

```
host# podman run --privileged --rm -v /var/srv:/srv quay.io/cgwalters/coreos-assembler
# cd /srv && coreos-assembler --repo=repo --cachedir=cache host.yml
```

Development
---

The container image is built in [OpenShift CI](https://api.ci.openshift.org/console/project/coreos/browse/builds/coreos-assembler?tab=history).
