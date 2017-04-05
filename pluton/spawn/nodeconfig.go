package spawn

var nodeTmpl = `#cloud-config
coreos:
  units: {{ if and .Master .StartEtcd }}
    - name: etcd-member.service
      command: start
      runtime: true
      drop-ins:
        - name: 40-etcd-cluster.conf
          content: |
            [Service]
            Environment="ETCD_IMAGE_TAG=v3.1.0"
            Environment="ETCD_NAME=controller"
            Environment="ETCD_ADVERTISE_CLIENT_URLS=http://$private_ipv4:2379"
            Environment="ETCD_INITIAL_ADVERTISE_PEER_URLS=http://$private_ipv4:2380"
            Environment="ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379"
            Environment="ETCD_LISTEN_PEER_URLS=http://0.0.0.0:2380"
            Environment="ETCD_INITIAL_CLUSTER=controller=http://$private_ipv4:2380"{{ end }}
    - name: kubelet.service
      enable: false
      content: | 
{{.KubeletService}}
  update:
    reboot-strategy: "off"`

var defaultKubeletMasterService = `[Service]
Environment=KUBELET_IMAGE_URL=quay.io/coreos/hyperkube
Environment=KUBELET_IMAGE_TAG=v1.5.6_coreos.0
Environment="RKT_RUN_ARGS=\
--uuid-file-save=/var/run/kubelet-pod.uuid \
--volume etc-resolv,kind=host,source=/etc/resolv.conf --mount volume=etc-resolv,target=/etc/resolv.conf \
--volume var-lib-cni,kind=host,source=/var/lib/cni --mount volume=var-lib-cni,target=/var/lib/cni"
EnvironmentFile=/etc/environment
ExecStartPre=/bin/mkdir -p /etc/kubernetes/manifests
ExecStartPre=/bin/mkdir -p /etc/kubernetes/cni/net.d
ExecStartPre=/bin/mkdir -p /etc/kubernetes/checkpoint-secrets
ExecStartPre=/bin/mkdir -p /srv/kubernetes/manifests
ExecStartPre=/bin/mkdir -p /var/lib/cni
ExecStartPre=-/usr/bin/rkt rm --uuid-file=/var/run/kubelet-pod.uuid
ExecStart=/usr/lib/coreos/kubelet-wrapper \
  --kubeconfig=/etc/kubernetes/kubeconfig \
  --require-kubeconfig \
  --client-ca-file=/etc/kubernetes/ca.crt \
  --anonymous-auth=false \
  --cni-conf-dir=/etc/kubernetes/cni/net.d \
  --network-plugin=cni \
  --lock-file=/var/run/lock/kubelet.lock \
  --exit-on-lock-contention \
  --allow-privileged \
  --hostname-override=${COREOS_PRIVATE_IPV4} \
  --node-labels=master=true \
  --minimum-container-ttl-duration=3m0s \
  --cluster_dns=10.3.0.10 \
  --cluster_domain=cluster.local \
  --config=/etc/kubernetes/manifests

ExecStop=-/usr/bin/rkt stop --uuid-file=/var/run/kubelet-pod.uuid
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target`

var defaultKubeletWorkerService = `[Service]
Environment=KUBELET_IMAGE_URL=quay.io/coreos/hyperkube
Environment=KUBELET_IMAGE_TAG=v1.5.6_coreos.0
Environment="RKT_RUN_ARGS=\
--uuid-file-save=/var/run/kubelet-pod.uuid \
--volume etc-resolv,kind=host,source=/etc/resolv.conf --mount volume=etc-resolv,target=/etc/resolv.conf \
--volume var-lib-cni,kind=host,source=/var/lib/cni --mount volume=var-lib-cni,target=/var/lib/cni"
EnvironmentFile=/etc/environment
ExecStartPre=/bin/mkdir -p /etc/kubernetes/manifests
ExecStartPre=/bin/mkdir -p /etc/kubernetes/cni/net.d
ExecStartPre=/bin/mkdir -p /var/lib/cni
ExecStartPre=-/usr/bin/rkt rm --uuid-file=/var/run/kubelet-pod.uuid
ExecStart=/usr/lib/coreos/kubelet-wrapper \
  --kubeconfig=/etc/kubernetes/kubeconfig \
  --require-kubeconfig \
  --client-ca-file=/etc/kubernetes/ca.crt \
  --anonymous-auth=false \
  --cni-conf-dir=/etc/kubernetes/cni/net.d \
  --network-plugin=cni \
  --lock-file=/var/run/lock/kubelet.lock \
  --exit-on-lock-contention \
  --allow-privileged \
  --hostname-override=${COREOS_PRIVATE_IPV4} \
  --minimum-container-ttl-duration=3m0s \
  --cluster_dns=10.3.0.10 \
  --cluster_domain=cluster.local \
  --config=/etc/kubernetes/manifests

ExecStop=-/usr/bin/rkt stop --uuid-file=/var/run/kubelet-pod.uuid
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target`
