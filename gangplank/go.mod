module github.com/coreos/gangplank

go 1.15

require (
	github.com/containerd/containerd v1.5.8 // indirect
	github.com/containers/podman/v3 v3.2.1
	github.com/containers/storage v1.38.1
	github.com/coreos/coreos-assembler-schema v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.2.0
	github.com/minio/minio-go/v7 v7.0.12
	github.com/opencontainers/runc v1.1.0
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417
	github.com/openshift/api v0.0.0-20210521075222-e273a339932a
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)

replace github.com/coreos/coreos-assembler-schema => ../schema
