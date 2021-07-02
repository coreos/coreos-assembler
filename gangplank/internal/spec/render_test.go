package spec

import (
	"context"
	"io/ioutil"
	"os"
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

func wantedGot(want, got interface{}, t *testing.T) {
	if want != got {
		t.Errorf("wanted: %v\n   got: %v", want, got)
	}
}

func TestJobSpec(t *testing.T) {
	rd := new(RenderData)

	if err := yaml.Unmarshal([]byte(MockOSJobSpec), &rd.JobSpec); err != nil {
		t.Errorf("failed to read mock jobspec")
	}

	wantedGot("mockOS-99", rd.JobSpec.Job.BuildName, t)

	// Test rendering from a string
	s, err := rd.ExecuteTemplateFromString("good {{ .JobSpec.Job.BuildName }}")
	wantedGot(nil, err, t)
	wantedGot("good mockOS-99", s[0], t)
	wantedGot(1, len(s), t)

	// Test rendering for a slice of strings
	s, err = rd.ExecuteTemplateFromString("good", "{{ .JobSpec.Job.BuildName }}")
	wantedGot(nil, err, t)
	wantedGot("mockOS-99", s[1], t)
	wantedGot(2, len(s), t)

	// Test a failure
	_, err = rd.ExecuteTemplateFromString("this", "wont", "{{ .Work }}")
	if err == nil {
		t.Errorf("template should not render")
	}

	// Test a script
	f, err := ioutil.TempFile("", "meh")
	defer os.Remove(f.Name())
	wantedGot(nil, err, t)
	err = ioutil.WriteFile(f.Name(), []byte("echo {{ .JobSpec.Job.BuildName }}"), 0444)
	wantedGot(nil, err, t)

	ctx := context.Background()
	err = rd.RendererExecuter(ctx, []string{}, f.Name())
	wantedGot(nil, err, t)

}
