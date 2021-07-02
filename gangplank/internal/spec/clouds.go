package spec

import (
	"fmt"
	"strings"
)

type Cloud interface {
	GetPublishCommand(string) (string, error)
}

// Aliyun is nested under CloudsCfgs and describes where
// the Aliyun/Alibaba artifacts should be uploaded to.
type Aliyun struct {
	Bucket  string   `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Enabled bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Public  bool     `yaml:"public,omitempty" json:"public,omitempty"`
	Regions []string `yaml:"regions,omitempty" json:"regions,omitempty"`
}

// GetPublishCommand returns the cosa upload command for Aliyun
func (a *Aliyun) GetPublishCommand(buildID string) (string, error) {
	if buildID == "" {
		return "", fmt.Errorf("No build provided")
	}

	if !a.Enabled {
		return "", nil
	}

	if a.Public {
		return "", fmt.Errorf("Public is not supported on Aliyun")
	}

	baseCmd := "coreos-assembler buildextend-aliyun"
	args := []string{"--upload",
		fmt.Sprintf("--build=%s", buildID),
		fmt.Sprintf("--bucket=s3://%s", a.Bucket)}

	if len(a.Regions) > 0 {
		args = append(args, fmt.Sprintf("--region=%s", a.Regions[0]))
	}

	cmd := fmt.Sprintf("%s %s", baseCmd, strings.Join(args, " "))
	return cmd, nil
}

// Aws describes the upload options for AWS images
//  AmiPath: the bucket patch for pushing the AMI name
//  Public: when true, mark as public
//  Geo: the abbreviated AWS region, i.e aws-cn would be `cn`
//  GrantUser: users to grant access to ami
//  GrantUserSnapshot: users to grant access to snapshot
//  Regions: name of AWS regions to push to.

type Aws struct {
	Enabled           bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AmiPath           string   `yaml:"ami_path,omitempty" json:"ami_path,omitempty"`
	Geo               string   `yaml:"geo,omitempty" json:"geo,omitempty"`
	GrantUser         []string `yaml:"grant_user,omitempty" json:"grant_user,omitempty"`
	GrantUserSnapshot []string `yaml:"grant_user_snapshot,omitempty" json:"grant_user_snapshot,omitempty"`
	Public            bool     `yaml:"public,omitempty" json:"public,omitempty"`
	Regions           []string `yaml:"regions,omitempty" json:"regions,omitempty"`
}

// GetPublishCommand returns the cosa upload command for Aws
func (a *Aws) GetPublishCommand(buildID string) (string, error) {
	if buildID == "" {
		return "", fmt.Errorf("no build provided")
	}

	if !a.Enabled {
		return "", nil
	}

	baseCmd := "coreos-assembler buildextend-aws"
	args := []string{"--upload",
		fmt.Sprintf("--build=%s", buildID),
		fmt.Sprintf("--bucket=s3://%s", a.AmiPath)}

	if len(a.Regions) > 0 {
		args = append(args, fmt.Sprintf("--region=%s", a.Regions[0]))
	}

	if len(a.GrantUser) > 0 {
		args = append(args, fmt.Sprintf("--grant-user %s", strings.Join(a.GrantUser, ",")))
	}

	if len(a.GrantUserSnapshot) > 0 {
		args = append(args, fmt.Sprintf("--grant-user-snapshot %s", strings.Join(a.GrantUserSnapshot, ",")))
	}

	var env string
	if a.Geo != "" {
		env = fmt.Sprintf("AWS_CONFIG_FILE=$AWS_%s_CONFIG_FILE",
			strings.ToUpper(a.Geo))
	}

	cmd := fmt.Sprintf("%s %s %s", env, baseCmd, strings.Join(args, " "))
	return cmd, nil
}

// Azure describes upload options for Azure images.
//   Enabled: upload if true
//   ResourceGroup: the name of the Azure resource group
//   StorageAccount: name of the storage account
//   StorageContainer: name of the storage container
//   StorageLocation: name of the Azure region, i.e. us-east-1
type Azure struct {
	Enabled          bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ResourceGroup    string `yaml:"resource_group,omitempty" json:"resource_group,omitempty"`
	StorageAccount   string `yaml:"storage_account,omitempty" json:"stoarge_account,omitempty"`
	StorageContainer string `yaml:"storage_container,omitempty" json:"storage_container,omitempty"`
	StorageLocation  string `yaml:"storage_location,omitempty" json:"storage_location,omitempty"`
	Force            bool   `yaml:"force,omitempty" json:"force,omitempty"`
}

// GetPublishCommand returns the cosa upload command for Azure
func (a *Azure) GetPublishCommand(buildID string) (string, error) {
	if buildID == "" {
		return "", fmt.Errorf("no build provided")
	}

	if !a.Enabled {
		return "", nil
	}

	baseCmd := "coreos-assembler buildextend-azure"
	args := []string{"--upload",
		fmt.Sprintf("--build %s", buildID),
		"--auth $AZURE_CONFIG",
		fmt.Sprintf("--container %s", a.StorageContainer),
		"--profile $AZURE_PROFILE",
		fmt.Sprintf("--resource-group %s", a.ResourceGroup),
		fmt.Sprintf("--storage-account %s", a.StorageAccount)}

	if a.Force {
		args = append(args, "--force")
	}

	cmd := fmt.Sprintf("%s %s", baseCmd, strings.Join(args, " "))
	return cmd, nil
}

// Gcp describes deploying to the GCP environment
//   Bucket: name of GCP bucket to store image in
//   Enabled: when true, publish to GCP
//   Project: name of the GCP project to use
//   CreateImage: Whether or not to create an image in GCP after upload
//   Deprecated: If the image should be marked as deprecated
//   Description: The description that should be attached to the image
//   Enabled: toggle for uploading to GCP
//   Family: GCP image family to attach image to
//   License: The licenses that should be attached to the image
//   LogLevel: log level--DEBUG, WARN, INFO
//   Project: GCP project name
//   Public: If the image should be given public ACLs
type Gcp struct {
	Bucket      string   `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	CreateImage bool     `yaml:"create_image" json:"create_image"`
	Deprecated  bool     `yaml:"deprecated" json:"deprecated"`
	Description string   `yaml:"description" json:"description"`
	Enabled     bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Family      string   `yaml:"family" json:"family"`
	License     []string `yaml:"license" json:"license"`
	LogLevel    string   `yaml:"log_level" json:"log_level"`
	Project     string   `yaml:"project,omitempty" json:"project,omitempty"`
	Public      bool     `yaml:"public,omitempty" json:"public,omitempty"`
}

// GetPublishCommand returns the cosa upload command for GCP
func (g *Gcp) GetPublishCommand(buildID string) (string, error) {
	if buildID == "" {
		return "", fmt.Errorf("no build provided")
	}

	if !g.Enabled {
		return "", nil
	}

	baseCmd := "coreos-assembler buildextend-gcp"
	args := []string{"--upload",
		fmt.Sprintf("--build %s", buildID),
		fmt.Sprintf("--project %s", g.Project),
		fmt.Sprintf("--bucket gs://%s", g.Bucket),
		"--json $GCP_IMAGE_UPLOAD_CONFIG"}

	if g.Public {
		args = append(args, "--public")
	}

	if g.CreateImage {
		args = append(args, "--create-image=true")
	}

	if g.Family != "" {
		args = append(args, fmt.Sprintf("--family %s", g.Family))
	}

	for _, f := range g.License {
		args = append(args, fmt.Sprintf("--license %s", f))
	}

	if g.Description != "" {
		args = append(args, fmt.Sprintf("--description %s", g.Description))
	}

	cmd := fmt.Sprintf("%s %s", baseCmd, strings.Join(args, " "))
	return cmd, nil
}
