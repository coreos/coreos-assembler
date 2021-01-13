module github.com/coreos/mantle

go 1.12

require (
	github.com/Azure/azure-sdk-for-go v8.1.0-beta+incompatible
	github.com/Azure/go-autorest v9.1.0+incompatible
	github.com/Microsoft/azure-vhd-utils v0.0.0-20161127050200-43293b8d7646
	github.com/ajeddeloh/yaml v0.0.0-20170912190910-6b94386aeefd // indirect
	github.com/aliyun/alibaba-cloud-sdk-go v0.0.0-20190929091402-5711055976b5
	github.com/aliyun/aliyun-oss-go-sdk v2.0.3+incompatible
	github.com/aws/aws-sdk-go v1.30.28
	github.com/coreos/container-linux-config-transpiler v0.8.0
	github.com/coreos/go-semver v0.3.0
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e
	github.com/coreos/go-systemd/v22 v22.0.0
	github.com/coreos/ign-converter v0.0.0-20201123014402-35e810ace1bf
	github.com/coreos/ignition v0.35.0
	github.com/coreos/ignition/v2 v2.9.0
	github.com/coreos/ioprogress v0.0.0-20151023204047-4637e494fd9b
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f
	github.com/coreos/stream-metadata-go v0.0.0-20210107232620-d808ce9d237c
	github.com/digitalocean/go-libvirt v0.0.0-20200810224808-b9c702499bf7 // indirect
	github.com/digitalocean/go-qemu v0.0.0-20200529005954-1b453d036a9c
	github.com/digitalocean/godo v1.33.0
	github.com/dimchansky/utfbom v1.1.0 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/gophercloud/gophercloud v0.13.0
	github.com/gophercloud/utils v0.0.0-20201101202656-8677e053dcf1
	github.com/kballard/go-shellquote v0.0.0-20150810074751-d8ec1a69a250
	github.com/kylelemons/godebug v0.0.0-20150519154555-21cb3784d9bd
	github.com/packethost/packngo v0.0.0-20180426081943-80f62d78849d
	github.com/pborman/uuid v1.2.0
	github.com/pin/tftp v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v0.0.6
	github.com/spf13/pflag v1.0.3
	github.com/ulikunitz/xz v0.5.4
	github.com/vincent-petithory/dataurl v0.0.0-20191104211930-d1553a71de50
	github.com/vishvananda/netlink v0.0.0-20150710184826-9cff81214893
	github.com/vishvananda/netns v0.0.0-20150710222425-604eaf189ee8
	github.com/vmware/govmomi v0.15.0
	github.com/xeipuuv/gojsonschema v1.2.0
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/net v0.0.0-20201021035429-f5854403a974
	golang.org/x/oauth2 v0.0.0-20200902213428-5d25da1a8d43
	golang.org/x/sys v0.0.0-20200930185726-fdedc70b468f
	golang.org/x/text v0.3.3
	google.golang.org/api v0.34.0
	gopkg.in/yaml.v2 v2.3.0
)

replace google.golang.org/cloud => cloud.google.com/go v0.0.0-20190220171618-cbb15e60dc6d
