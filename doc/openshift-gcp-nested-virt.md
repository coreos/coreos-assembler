Using OpenShift 4.2 in Google Compute Platform with nested virt
====

At the time of this writing, OpenShift 4.2 isn't released, but
the current development builds support Google Compute Platform.

First, stand up a 4.2 devel cluster in GCP.

Find the RHCOS image created by the installer (I browsed in the console, but
you can also use the `gcloud` CLI).  The image name will start with
a prefix of your cluster name.

Follow [the nested virt instructions](https://cloud.google.com/compute/docs/instances/enable-nested-virtualization-vm-instances) to create a new "blessed" image with the license:

```
gcloud compute images create walters-rhcos-nested-virt \
                                   --source-image walter-f57qc-rhcos-image --source-image-project openshift-gce-devel \
                                   --licenses "https://www.googleapis.com/compute/v1/projects/vm-options/global/licenses/enable-vmx"
```

One of the powerful advantages of OpenShift 4 is the machine API - you can dynamically reconfigure the workers
by editing a custom resource.

There are two approaches; you can [edit the existing machinesets](https://docs.openshift.com/container-platform/4.1/machine_management/modifying-machineset.html)
or create a new one.

Either way you choose, change the disk image:

```
          disks:
          - ...
            image: walters-rhcos-nested-virt
```

[Install the KVM device plugin](https://github.com/kubevirt/kubernetes-device-plugins/blob/master/docs/README.kvm.md) from KubeVirt.

Up to this point, you needed to be `kubeadmin`.
From this point on though, best practice is to switch to an "unprivileged" user.

(In fact the steps until this point could be run by a separate team
 that manages the cluster; other developers could just use it as unprivileged users)

Personally, I added a [httpasswd identity provider](https://docs.openshift.com/container-platform/4.1/authentication/identity_providers/configuring-htpasswd-identity-provider.html)
and logged in with a password.

I also did `oc new-project coreos-virt` etc.

Schedule a cosa pod:

```
apiVersion: v1
kind: Pod
metadata:
  labels:
    run: cosa
  name: cosa
spec:
  containers:
  - args:
    - shell
    - sleep
    - infinity
    image: quay.io/coreos-assembler/coreos-assembler:latest
    name: cosa
    resources:
      requests:
        # Today COSA hardcodes 2048 for launching VMs.  We could
        # probably shrink that in the future.
        memory: "3Gi"
        devices.kubevirt.io/kvm: "1"
      limits:
        memory: "3Gi"
        devices.kubevirt.io/kvm: "1"
    volumeMounts:
    - mountPath: /srv
      name: workdir
  volumes:
  - name: workdir
    emptyDir: {}
  restartPolicy: Never
```

Then `oc rsh pods/cosa` and you should be able to `ls -al /dev/kvm` - and `cosa build` etc!
