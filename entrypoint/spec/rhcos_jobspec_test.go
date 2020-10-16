package spec

import (
	"testing"

	"gopkg.in/yaml.v2"
)

// MockOSJobSpec is an example jobSpec for testing
var MockOSJobSpec = `
clouds_cfgs:
  aws:
    ami_path: mock-ami/testing/amis
    public: false
    regions:
      - us-east-1
  azure:
    enabled: true
    resource_group: os4-common
    secret_name: mockOS-azure
    storage_account: mock
    storage_container: imagebucket
    storage_location: eastus2
  gcp:
    enabled: true
    bucket: mockOS-devel/devel
    platform_id: gce
    project: openshift-mockOS-devel
    secret_name: mockOS-gce-service-account
    secret_payload: gce.json
  aliyun:
    enabled: false
    bucket: mockOS-images
    regions:
      - us-west-1
job:
  build_name: "mockOS-99"
  force_version: "99.99.999999999999"
  version_suffix: "magical-unicorns"
recipe:
  git_ref: "mock"
  git_url: https://github.com/coreos/coreos-assembler
oscontainer:
  push_url: registry.mock.example.org/mockOS-devel/machine-os-content
`

func wantedGot(want, got string, t *testing.T) {
	if want != got {
		t.Errorf("wanted: %s\n   got: %s", want, got)
	}
}

func TestJobSpec(t *testing.T) {
	var js JobSpec

	if err := yaml.Unmarshal([]byte(MockOSJobSpec), &js); err != nil {
		t.Errorf("failed to read mock jobspec")
	}

	wantedGot("mockOS-99", js.Job.BuildName, t)
}
