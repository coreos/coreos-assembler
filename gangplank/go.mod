module github.com/coreos/gangplank

go 1.15

require (
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/containerd/containerd v1.6.1 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.11.2 // indirect
	github.com/containers/buildah v1.23.3 // indirect
	github.com/containers/image/v5 v5.20.0 // indirect
	github.com/containers/podman/v3 v3.4.3
	github.com/containers/psgo v1.7.2 // indirect
	github.com/containers/storage v1.38.2
	github.com/coreos/coreos-assembler-schema v0.0.0-00010101000000-000000000000
	github.com/cri-o/ocicni v0.2.1-0.20211005135702-b38844812e64 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/docker/go-connections v0.4.1-0.20210727194412-58542c764a11 // indirect
	github.com/google/go-cmp v0.5.7 // indirect
	github.com/google/uuid v1.3.0
	github.com/jinzhu/copier v0.3.5 // indirect
	github.com/miekg/pkcs11 v1.1.1 // indirect
	github.com/minio/minio-go/v7 v7.0.12
	github.com/moby/sys/mountinfo v0.6.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.18.1 // indirect
	github.com/opencontainers/image-spec v1.0.3-0.20220303224323-02efb9a75ee1 // indirect
	github.com/opencontainers/runc v1.1.0
	github.com/opencontainers/runtime-spec v1.0.3-0.20220225203953-7ceeb8af5259
	github.com/openshift/api v0.0.0-20210521075222-e273a339932a
	github.com/prometheus/client_golang v1.12.1 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.3.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/vbauerster/mpb/v7 v7.4.1 // indirect
	github.com/vishvananda/netlink v1.1.1-0.20220115184804-dd687eb2f2d4 // indirect
	go.mozilla.org/pkcs7 v0.0.0-20210826202110-33d05740a352 // indirect
	golang.org/x/crypto v0.0.0-20220307211146-efcb8507fb70
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f // indirect
	golang.org/x/sys v0.0.0-20220307203707-22a9840ba4d7 // indirect
	google.golang.org/genproto v0.0.0-20220308174144-ae0e22291548 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.5
	k8s.io/apimachinery v0.22.5
	k8s.io/client-go v0.22.5
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace github.com/coreos/coreos-assembler-schema => ../schema
