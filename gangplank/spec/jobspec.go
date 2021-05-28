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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v2"
)

// JobSpec is the root-level item for the JobSpec.
type JobSpec struct {
	Archives   Archives   `yaml:"archives,omitempty" json:"archives,omitempty"`
	CloudsCfgs CloudsCfgs `yaml:"clouds_cfgs,omitempty" json:"cloud_cofgs,omitempty"`
	Job        Job        `yaml:"job,omitempty" json:"job,omitempty"`
	Recipe     Recipe     `yaml:"recipe,omitempty" json:"recipe,omitempty"`
	Spec       Spec       `yaml:"spec,omitempty" json:"spec,omitempty"`

	// PublishOscontainer is a list of push locations for the oscontainer
	PublishOscontainer PublishOscontainer `yaml:"publish_oscontainer,omitempty" json:"publish_oscontainer,omitempty"`

	// Stages are specific stages to be run. Stages are
	// only supported by Gangplank; they do not appear in the
	// Groovy Jenkins Scripts.
	Stages []Stage `yaml:"stages" json:"stages"`

	// DelayedMetaMerge ensures that 'cosa build' is called with
	// --delayed-meta-merge
	DelayedMetaMerge bool `yaml:"delay_meta_merge" json:"delay_meta_meta,omitempty"`
}

// Artifacts describe the expect build outputs.
//  All: name of the all the artifacts
//  Primary: Non-cloud builds
//  Clouds: Cloud publication stages.
type Artifacts struct {
	All     []string `yaml:"all,omitempty" json:"all,omitempty"`
	Primary []string `yaml:"primary,omitempty" json:"primary,omitempty"`
	Clouds  []string `yaml:"clouds,omitempty" json:"clouds,omitempty"`
}

// Archives describes the location of artifacts to push to
//   Brew is a nested Brew struct
//   S3: publish to S3.
type Archives struct {
	Brew *Brew `yaml:"brew,omitempty" json:"brew,omitempty"`
	S3   *S3   `yaml:"s3,omitempty" json:"s3,omitempty"`
}

// Brew is the RHEL Koji instance for storing artifacts.
// 	 Principle: the Kerberos user
//   Profile: the profile to use, i.e. brew-testing
//   Tag: the Brew tag to tag the build as.
type Brew struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Principle string `yaml:"principle,omitempty" json:"principle,omitempty"`
	Profile   string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Tag       string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

// CloudsCfgs (yes Clouds) is a nested struct of all
// supported cloudClonfigurations.
type CloudsCfgs struct {
	Aliyun *Aliyun `yaml:"aliyun,omitempty" json:"aliyun,omitempty"`
	Aws    *Aws    `yaml:"aws,omitempty" json:"aws,omitempty"`
	AwsCn  *Aws    `yaml:"aws-cn,omitempty" json:"aws-cn,omitempty"`
	Azure  *Azure  `yaml:"azure,omitempty" json:"azure,omitempty"`
	Gcp    *Gcp    `yaml:"gcp,omitempty" json:"gcp,omitempty"`
}

// getCloudsCfgs returns list of clouds that are defined in the jobspec. Since
// omitempty is used when unmarshaling some objects will not be available
func (c *CloudsCfgs) GetCloudCfg(cloud string) (Cloud, error) {
	t := reflect.TypeOf(*c)
	v := reflect.ValueOf(*c)
	for i := 0; i < t.NumField(); i++ {
		fieldName := strings.ToLower(t.Field(i).Name)
		if strings.ReplaceAll(cloud, "-", "") == fieldName {
			if ci, ok := v.Field(i).Interface().(Cloud); ok {
				return ci, nil
			}
			return nil, fmt.Errorf("failed casting struct to Cloud interface for %q cloud", cloud)
		}
	}
	return nil, fmt.Errorf("Could not find cloud config %s", cloud)
}

// Job refers to the Jenkins options
//   BuildName: i.e. rhcos-4.7
//   IsProduction: enforce KOLA tests
//   StrictMode: only run explicitly defined stages
//   VersionSuffix: name to append, ie. devel
type Job struct {
	BuildName     string `yaml:"build_name,omitempty" json:"build_name,omitempty"`
	IsProduction  bool   `yaml:"is_production,omitempty" json:"is_production,omitempty"`
	StrictMode    bool   `yaml:"strict,omitempty" json:"strict,omitempty"`
	VersionSuffix string `yaml:"version_suffix,omitempty" json:"version_suffix,omitempty"`
	// ForceArch forces a specific architecutre.
	ForceArch string `yaml:"force_arch,omitempty" json:"force_arch,omitempty"`
	// Unexported minio valued (run-time options)
	MinioCfgFile string // not exported

	// Runtime config options for SSH. Not exported for safety.
	MinioSSHForward string
	MinioSSHUser    string
}

// Recipe describes where to get the build recipe/config, i.e fedora-coreos-config
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Recipe struct {
	GitRef string  `yaml:"git_ref,omitempty" json:"git_ref,omitempty"`
	GitURL string  `yaml:"git_url,omitempty" json:"git_url,omitempty"`
	Repos  []*Repo `yaml:"repos,omitempty" json:"repos,omitempty"`
}

// Repo is a yum/dnf repositories to use as an installation source.
type Repo struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// URL indicates that the repo file is remote
	URL *string `yaml:"url,omitempty" json:"url,omitempty"`

	// Inline indicates that the repo file is inline
	Inline *string `yaml:"inline,omitempty" json:"inline,omitempty"`
}

// Writer places the remote repo file into path. If the repo has no name,
// then a SHA256 of the URL will be used. Returns path of the file and err.
func (r *Repo) Writer(path string) (string, error) {
	if r.URL == nil && r.Inline == nil {
		return "", errors.New("repo must be a URL or inline data")
	}
	rname := r.Name
	var data string
	if r.URL != nil {
		data = *r.URL
	} else {
		data = *r.Inline
	}
	if rname == "" {
		h := sha256.New()
		if _, err := h.Write([]byte(data)); err != nil {
			return "", fmt.Errorf("failed to calculate name: %v", err)
		}
		rname = fmt.Sprintf("%x", h.Sum(nil))
	}

	f := filepath.Join(path, fmt.Sprintf("%s.repo", rname))
	out, err := os.OpenFile(f, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return f, fmt.Errorf("failed to open %s for writing: %v", f, err)
	}
	defer out.Close()

	closer := func() error { return nil }
	var dataR io.Reader
	if r.URL != nil && *r.URL != "" {
		resp, err := http.Get(*r.URL)
		if err != nil {
			return f, err
		}
		dataR = resp.Body
		closer = resp.Body.Close
	} else {
		dataR = strings.NewReader(*r.Inline)
	}

	defer closer() //nolint

	n, err := io.Copy(out, dataR)
	if n == 0 {
		return f, errors.New("No remote content fetched")
	}
	return f, err
}

// S3 describes the location of the S3 Resource.
//   Acl: is the s3 acl to use, usually 'private' or 'public'
//   Bucket: name of the S3 bucket
//   Path: the path inside the bucket
type S3 struct {
	ACL    string `yaml:"acl,omitempty" envVar:"S3_ACL" json:"acl,omitempty"`
	Bucket string `yaml:"bucket,omitempty" envVar:"S3_BUCKET" json:"bucket,omitempty"`
	Path   string `yaml:"path,omitempty" envVar:"S3_PATH" json:"path,omitempty"`
}

// Spec describes the RHCOS JobSpec.
//   GitRef: branch/ref to fetch from
//   GitUrl: url of the repo
type Spec struct {
	GitRef string `yaml:"git_ref,omitempty" json:"git_ref,omitempty"`
	GitURL string `yaml:"git_url,omitempty" json:"git_url,omitempty"`
}

// PublishOscontainer describes where to push the OSContainer to.
type PublishOscontainer struct {
	// BuildStrategyTLSVerify indicates whether to verify TLS certificates when pushing as part of a OCP Build Strategy.
	// By default, TLS verification is turned on.
	BuildStrategyTLSVerify *bool `yaml:"buildstrategy_tls_verify" json:"buildstrategy_tls_verify"`

	// Registries is a list of locations to push to.
	Registries []Registry `yaml:"registries" json:"regristries"`
}

// Registry describes the push locations.
type Registry struct {
	// URL is the location that should be used to push the secret.
	URL string `yaml:"url" json:"url"`

	// TLSVerify tells when to verify TLS. By default, its true
	TLSVerify *bool `yaml:"tls_verify,omitempty" json:"tls_verify,omitempty"`

	// SecretType is name the secret to expect, should PushSecretType*s
	SecretType PushSecretType `yaml:"secret_type,omitempty" json:"secret_type,omitempty"`

	// If the secret is inline, the string data, else, the cluster secret name
	Secret string `yaml:"secret,omitempty" json:"secret,omitempty"`
}

// PushSecretType describes the type of push secret.
type PushSecretType string

// Supported push secret types.
const (
	// PushSecretTypeInline means that the secret string is a string literal
	// of the docker auth.json.
	PushSecretTypeInline = "inline"
	// PushSecretTypeCluster indicates that the named secret in PushRegistry should be
	// fetched via the service account from the cluster.
	PushSecretTypeCluster = "cluster"
	// PushSecretTypeToken indicates that the service account associated with the token
	// has access to the push repository.
	PushSecretTypeToken = "token"
)

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

// WriteJSON returns the jobspec
func (js *JobSpec) WriteJSON(w io.Writer) error {
	encode := json.NewEncoder(w)
	encode.SetIndent("", "  ")
	return encode.Encode(js)
}

// WriteYAML returns the jobspec in YAML
func (js *JobSpec) WriteYAML(w io.Writer) error {
	encode := yaml.NewEncoder(w)
	defer encode.Close()
	return encode.Encode(js)
}
