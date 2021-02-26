module github.com/coreos/gangplank

go 1.13

require (
	github.com/containers/libpod v1.9.3
	github.com/containers/storage v1.20.2
	github.com/minio/minio-go/v7 v7.0.6
	github.com/opencontainers/runc v1.0.0-rc90
	github.com/opencontainers/runtime-spec v1.0.3-0.20200520003142-237cc4f519e2
	github.com/openshift/api v0.0.0-20201119214056-f1dea5ee7f60
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/xeipuuv/gojsonschema v1.2.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.19.4
	k8s.io/apimachinery v0.19.4
	k8s.io/client-go v11.0.0+incompatible
)

replace (
	github.com/containers/storage => github.com/containers/storage v1.20.2
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.0
	k8s.io/api => k8s.io/api v0.17.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.0
	k8s.io/client-go => k8s.io/client-go v0.17.0
)
