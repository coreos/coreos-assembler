package spec

import (
	"fmt"
	"reflect"
	"strings"
)

// PublishArtifact describes a cloud artifact that will
// be pushed to the cloud.
type PublishArtifact struct {
	// Variable names that occur in more than one cloud struct must be declared
	// here because the inline will cause a panic when unmarshalling

	// Common to all clouds
	Name      string `yaml:"cloud"`
	Enabled   bool   `yaml:"enabled" envVar:"ENABLED"`
	ExtraArgs string `yaml:"extra_args,omitempty" envVar:"EXTRA_ARGS"`

	// Cloud specific
	Aliyun Aliyun `yaml:",inline"`
	Aws    Aws    `yaml:",inline"`
	Azure  Azure  `yaml:",inline"`
	Gcp    Gcp    `yaml:",inline"`

	// Common to Gcp, Aws, and Aliyun
	Bucket  string   `yaml:"bucket,omitempty" envVar:"BUCKET"`
	Regions []string `yaml:"regions,omitempty" envVar:"REGIONS"`
}

// GetPublishCommand returns the set of commands to upload an image to the
// cloud provider
func (p PublishArtifact) GetPublishCommand() (string, error) {
	switch p.Name {
	case "aws":
		return fmt.Sprintf("PATH=$PATH:%s upload-ami --build $COSA_BUILD --region $COSA_AWS_REGIONS --bucket=s3://$COSA_AWS_AMI_PATH --skip-kola", "/srv/src/config/scripts"), nil
	case "aws-cn":
		return "upload-ami", nil
	case "aliyun":
		return "upload-ami", nil
	case "gcp":
		return "upload-ami", nil
	default:
		return "", fmt.Errorf("Not a valid cloud provider")
	}
}

// GetEnvVars returns a set of environment variable from the yaml struct tags
func (p *PublishArtifact) GetEnvVars() (envVars []string) {
	var getNestedEnvTags func(interface{})

	getNestedEnvTags = func(i interface{}) {
		typ := reflect.TypeOf(i)
		val := reflect.ValueOf(i)
		if typ.Kind() == reflect.Struct {
			for j := 0; j < typ.NumField(); j++ {
				field := typ.Field(j)
				fieldValue := val.Field(j)
				if field.Type.Kind() == reflect.Struct {
					getNestedEnvTags(fieldValue.Interface())
				} else {
					tag := field.Tag.Get("envVar")
					if tag == "" {
						continue
					}
					if !val.Field(j).IsZero() {
						// Prefix environment variables with COSA_CLOUDNAME_
						// so the cloud upload scripts can use it
						envVars = append(envVars, fmt.Sprintf("COSA_%s_%s=%v", strings.ToUpper(p.Name), tag, fieldValue.Interface()))
					}
				}
			}
		}
	}

	getNestedEnvTags(*p)

	return envVars
}
