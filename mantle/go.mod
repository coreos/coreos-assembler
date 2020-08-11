module github.com/coreos/mantle

go 1.12

require (
	github.com/Azure/azure-sdk-for-go v8.1.0-beta+incompatible
	github.com/Azure/go-autorest v9.1.0+incompatible
	github.com/Microsoft/azure-vhd-utils v0.0.0-20161127050200-43293b8d7646
	github.com/ajeddeloh/yaml v0.0.0-20170912190910-6b94386aeefd // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/aliyun/alibaba-cloud-sdk-go v0.0.0-20190929091402-5711055976b5
	github.com/aliyun/aliyun-oss-go-sdk v2.0.3+incompatible
	github.com/aws/aws-sdk-go v1.30.28
	github.com/coreos/container-linux-config-transpiler v0.8.0
	github.com/coreos/go-semver v0.3.0
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e
	github.com/coreos/go-systemd/v22 v22.0.0
	github.com/coreos/ign-converter v0.0.0-20200706175125-8bea9183aea9
	github.com/coreos/ignition v0.35.0
	github.com/coreos/ignition/v2 v2.6.0
	github.com/coreos/ioprogress v0.0.0-20151023204047-4637e494fd9b
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f
	github.com/coreos/vcontext v0.0.0-20191017033345-260217907eb5 // indirect
	github.com/digitalocean/godo v1.33.0
	github.com/dimchansky/utfbom v1.1.0 // indirect
	github.com/gedex/inflector v0.0.0-20170307190818-16278e9db813
	github.com/godbus/dbus v0.0.0-20181025153459-66d97aec3384 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/gophercloud/gophercloud v0.0.0-20180817041643-185230dfbd12
	github.com/idubinskiy/schematyper v0.0.0-20190118213059-f71b40dac30d
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
	golang.org/x/net v0.0.0-20200625001655-4c5254603344
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sys v0.0.0-20200610111108-226ff32320da
	golang.org/x/text v0.3.2
	google.golang.org/api v0.29.0
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
)

replace google.golang.org/cloud => cloud.google.com/go v0.0.0-20190220171618-cbb15e60dc6d
