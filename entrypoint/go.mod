module github.com/coreos/entrypoint

go 1.14

require (
	github.com/minio/minio v0.0.0-20201021042824-5cc23ae0526d // indirect
	github.com/minio/minio-go/v7 v7.0.6-0.20200929220449-755b5633803a
	github.com/openshift/api v0.0.0-20201005153912-821561a7f2a2
	github.com/openshift/client-go v3.9.0+incompatible
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.0.0
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v0.0.0-00010101000000-000000000000
	k8s.io/utils v0.0.0-20201005171033-6301aaf42dc7 // indirect

)

replace (
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.0
	k8s.io/api => k8s.io/api v0.17.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.0
	k8s.io/client-go => k8s.io/client-go v0.17.0
)
