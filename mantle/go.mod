module github.com/coreos/mantle

go 1.17

require (
	github.com/Azure/azure-sdk-for-go v8.1.0-beta+incompatible
	github.com/Azure/go-autorest v9.1.0+incompatible
	github.com/IBM-Cloud/bluemix-go v0.0.0-20210419045805-b50610722085
	github.com/IBM/ibm-cos-sdk-go v1.6.1
	github.com/Microsoft/azure-vhd-utils v0.0.0-20161127050200-43293b8d7646
	github.com/aliyun/alibaba-cloud-sdk-go v1.61.1442
	github.com/aliyun/aliyun-oss-go-sdk v2.0.3+incompatible
	github.com/aws/aws-sdk-go v1.34.28
	github.com/coreos/butane v0.16.0
	github.com/coreos/coreos-assembler v0.14.0
	github.com/coreos/go-semver v0.3.0
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e
	github.com/coreos/go-systemd/v22 v22.4.0
	github.com/coreos/ignition/v2 v2.14.0
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f
	github.com/coreos/stream-metadata-go v0.4.0
	github.com/coreos/vcontext v0.0.0-20220810162454-88bd546c634c
	github.com/digitalocean/go-qemu v0.0.0-20200529005954-1b453d036a9c
	github.com/digitalocean/godo v1.33.0
	github.com/gophercloud/gophercloud v0.22.0
	github.com/gophercloud/utils v0.0.0-20210323225332-7b186010c04f
	github.com/kballard/go-shellquote v0.0.0-20150810074751-d8ec1a69a250
	github.com/kylelemons/godebug v0.0.0-20150519154555-21cb3784d9bd
	github.com/packethost/packngo v0.0.0-20180426081943-80f62d78849d
	github.com/pborman/uuid v1.2.0
	github.com/pin/tftp v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v1.5.0
	github.com/vincent-petithory/dataurl v1.0.0
	github.com/vishvananda/netlink v0.0.0-20150710184826-9cff81214893
	github.com/vishvananda/netns v0.0.0-20150710222425-604eaf189ee8
	github.com/vmware/govmomi v0.15.0
	golang.org/x/crypto v0.0.0-20220315160706-3147a52a75dd
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2
	golang.org/x/oauth2 v0.0.0-20200902213428-5d25da1a8d43
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1
	golang.org/x/text v0.3.6
	google.golang.org/api v0.34.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	cloud.google.com/go v0.65.0 // indirect
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/clarketm/json v1.17.1 // indirect
	github.com/coreos/go-json v0.0.0-20220810161552-7cce03887f34 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/digitalocean/go-libvirt v0.0.0-20200810224808-b9c702499bf7 // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.4.2 // indirect
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/uuid v1.1.1 // indirect
	github.com/googleapis/gax-go/v2 v2.0.5 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace // indirect
	github.com/stretchr/testify v1.8.0 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	go.opencensus.io v0.22.5 // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/genproto v0.0.0-20200904004341-0bd0a958aa1d // indirect
	google.golang.org/grpc v1.31.1 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/ini.v1 v1.66.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/coreos/coreos-assembler => ../
	google.golang.org/cloud => cloud.google.com/go v0.0.0-20190220171618-cbb15e60dc6d
)
