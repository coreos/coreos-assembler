module github.com/coreos/gangplank

go 1.14

require (
	github.com/golang/protobuf v1.4.2 // indirect
	github.com/google/go-cmp v0.5.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/minio/minio-go/v7 v7.0.6-0.20200929220449-755b5633803a
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/openshift/api v0.0.0-20201005153912-821561a7f2a2
	github.com/pkg/errors v0.8.1
	github.com/sirupsen/logrus v1.7.0
	github.com/smartystreets/assertions v1.0.1 // indirect
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.6.1 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a // indirect
	golang.org/x/net v0.0.0-20200904194848-62affa334b73 // indirect
	golang.org/x/sys v0.0.0-20201009025420-dfb3f7c4e634 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.19.0
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
