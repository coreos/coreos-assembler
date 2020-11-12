/*
	The RHCOS JobSpec is a YAML file describing the various Jenkins Job
	knobs for controlling Pipeline execution. The JobSpec pre-dates this
	code, and has been in production since 2019.

	The JobSpec has considerably more options than reflected in this file.

	Only include options that are believed to be relavent to COSA
*/

package spec

import (
	"bufio"
	"io"
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v2"
)

// JobSpec is the root-level item for the JobSpec.
type JobSpec struct {
	Archives    Archives    `yaml:"archives,omitempty"`
	CloudsCfgs  CloudsCfgs  `yaml:"clouds_cfgs,omitempty"`
	Job         Job         `yaml:"job,omitempty"`
	Oscontainer Oscontainer `yaml:"oscontainer,omitempty"`
	Recipe      Recipe      `yaml:"recipe,omitempty"`
	Spec        Spec        `yaml:"spec,omitempty"`

	// Stages are specific stages to be run. Stages are
	// only supported by Gangplank; they do not appear in the
	// Groovy Jenkins Scripts.
	Stages []Stage `yaml:"stages,omitempty"`
}

// Artifacts describe the expect build outputs.
//  All: name of the all the artifacts
//  Primary: Non-cloud builds
//  Clouds: Cloud publication stages.
type Artifacts struct {
	All     []string `yaml:"all,omitempty"`
	Primary []string `yaml:"primary,omitempty"`
	Clouds  []string `yaml:"clouds,omitempty"`
}

// Aliyun is nested under CloudsCfgs and describes where
// the Aliyun/Alibaba artifacts should be uploaded to.
type Aliyun struct {
	Bucket  string   `yaml:"bucket,omitempty"`
	Enabled bool     `yaml:"enabled,omitempty"`
	Regions []string `yaml:"regions,omitempty"`
}

// Archives describes the location of artifacts to push to
//   Brew is a nested Brew struct
//   S3: publish to S3.
type Archives struct {
	Brew *Brew `yaml:"brew,omitempty"`
	S3   *S3   `yaml:"s3,omitempty"`
}

// Aws describes the upload options for AWS images
//  AmiPath: the bucket patch for pushing the AMI name
//  Public: when true, mark as public
//  Regions: name of AWS regions to push to.
type Aws struct {
	Enabled bool     `yaml:"enabled,omitempty"`
	AmiPath string   `yaml:"ami_path,omitempty"`
	Public  bool     `yaml:"public,omitempty"`
	Regions []string `yaml:"regions,omitempty"`
}

// Azure describes upload options for Azure images.
//   Enabled: upload if true
//   ResourceGroup: the name of the Azure resource group
//   StorageAccount: name of the storage account
//   StorageContainer: name of the storage container
//   StorageLocation: name of the Azure region, i.e. us-east-1
type Azure struct {
	Enabled          bool   `yaml:"enabled,omitempty"`
	ResourceGroup    string `yaml:"resource_group,omitempty"`
	StorageAccount   string `yaml:"storage_account,omitempty"`
	StorageContainer string `yaml:"storage_container,omitempty"`
	StorageLocation  string `yaml:"storage_location,omitempty"`
}

// Brew is the RHEL Koji instance for storing artifacts.
// 	 Principle: the Kerberos user
//   Profile: the profile to use, i.e. brew-testing
//   Tag: the Brew tag to tag the build as.
type Brew struct {
	Enabled   bool   `yaml:"enabled,omitempty"`
	Principle string `yaml:"principle,omitempty"`
	Profile   string `yaml:"profile,omitempty"`
	Tag       string `yaml:"tag,omitempty"`
}

// CloudsCfgs (yes Clouds) is a nested struct of all
// supported cloudClonfigurations.
type CloudsCfgs struct {
	Aliyun Aliyun `yaml:"aliyun,omitempty"`
	Aws    Aws    `yaml:"aws,omitempty"`
	Azure  Azure  `yaml:"azure,omitempty"`
	Gcp    Gcp    `yaml:"gcp,omitempty"`
}

// Gcp describes deploiying to the GCP environment
//   Bucket: name of GCP bucket to store image in
//   Enabled: when true, publish to GCP
//   Project: name of the GCP project to use
type Gcp struct {
	Bucket  string `yaml:"bucket,omitempty"`
	Enabled bool   `yaml:"enabled,omitempty"`
	Project string `yaml:"project,omitempty"`
}

// Job refers to the Jenkins options
//   BuildName: i.e. rhcos-4.7
//   IsProduction: enforce KOLA tests
//   StrictMode: only run explicitly defined stages
//   VersionSuffix: name to append, ie. devel
type Job struct {
	BuildName     string `yaml:"build_name,omitempty"`
	IsProduction  bool   `yaml:"is_production,omitempty"`
	StrictMode    bool   `yaml:"strict,omitempty"`
	VersionSuffix string `yaml:"version_suffix,omitempty"`
}

// Recipe describes where to get the build recipe/config, i.e fedora-coreos-config
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Recipe struct {
	GitRef string `yaml:"git_ref,omitempty"`
	GitURL string `yaml:"git_url,omitempty"`
}

// S3 describes the location of the S3 Resource.
//   Acl: is the s3 acl to use, usually 'private' or 'public'
//   Bucket: name of the S3 bucket
//   Path: the path inside the bucket
type S3 struct {
	ACL    string `yaml:"acl,omitempty" envVar:"S3_ACL"`
	Bucket string `yaml:"bucket,omitempty" envVar:"S3_BUCKET"`
	Path   string `yaml:"path,omitempty" envVar:"S3_PATH"`
}

// Spec describes the RHCOS JobSpec.
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Spec struct {
	GitRef string `yaml:"git_ref,omitempty"`
	GitURL string `yaml:"git_url,omitempty"`
}

// Oscontainer describes the location to push the OS Container to.
type Oscontainer struct {
	PushURL string `yaml:"push_url,omitempty"`
}

// JobSpecReader takes and io.Reader and returns a ptr to the JobSpec and err
func JobSpecReader(in io.Reader) (j JobSpec, err error) {
	d, err := ioutil.ReadAll(in)
	if err != nil {
		return j, err
	}

	err = yaml.Unmarshal(d, &j)
	if err != nil {
		return j, err
	}
	return j, err
}

// JobSpecFromFile return a JobSpec read from a file
func JobSpecFromFile(f string) (j JobSpec, err error) {
	in, err := os.Open(f)
	if err != nil {
		return j, err
	}
	defer in.Close()
	b := bufio.NewReader(in)
	return JobSpecReader(b)
}
