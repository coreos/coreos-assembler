package kubernetes

const rktkube = `#!/bin/bash
# Wrapper for launching kubelet via rkt-fly stage1

set -e

if [ -z "${KUBELET_VERSION}" ]; then
    echo "ERROR: must set KUBELET_VERSION"
    exit 1
fi

KUBELET_ACI="${KUBELET_ACI:-quay.io/coreos/hyperkube}"

mkdir --parents /etc/kubernetes
mkdir --parents /var/lib/docker
mkdir --parents /var/lib/kubelet
mkdir --parents /run/kubelet

exec /usr/bin/rkt run \
  --volume etc-kubernetes,kind=host,source=/etc/kubernetes \
  --volume etc-ssl-certs,kind=host,source=/usr/share/ca-certificates \
  --volume var-lib-docker,kind=host,source=/var/lib/docker \
  --volume var-lib-kubelet,kind=host,source=/var/lib/kubelet \
  --volume run,kind=host,source=/run \
  --mount volume=etc-kubernetes,target=/etc/kubernetes \
  --mount volume=etc-ssl-certs,target=/etc/ssl/certs \
  --mount volume=var-lib-docker,target=/var/lib/docker \
  --mount volume=var-lib-kubelet,target=/var/lib/kubelet \
  --mount volume=run,target=/run \
  --trust-keys-from-https \
  $RKT_OPTS \
  --stage1-path=/usr/share/rkt/stage1-fly.aci \
  ${KUBELET_ACI}:${KUBELET_VERSION} --exec=/kubelet -- "$@"`
