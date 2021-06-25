module github.com/coreos/gangplank

go 1.15

require (
	github.com/containers/podman/v3 v3.1.2
	github.com/containers/storage v1.29.0
	github.com/google/uuid v1.2.0
	github.com/minio/minio-go/v7 v7.0.11
	github.com/opencontainers/runc v1.0.0-rc93
	github.com/opencontainers/runtime-spec v1.0.3-0.20200929063507-e6143ca7d51d
	github.com/openshift/api v0.0.0-20210521075222-e273a339932a
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/xeipuuv/gojsonschema v1.2.0
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
	k8s.io/kubernetes v1.13.0
)
