// See usage below
package main

// build-cimage is a wrapper for `rpm-ostree compose image`; for more
// on that, see https://coreos.github.io/rpm-ostree/container/
//
// A key motivation here is to sever the dependency on S3 (and meta.json) for
// our container image builds.  As part of the ostree native container work,
// the core of CoreOS becomes a container image.  Our disk images
// have always been derivatives of the container, and this is pushing things
// farther in that direction.
// See https://github.com/coreos/coreos-assembler/issues/2685
//
// This command is opinionated on reading and writing to a remote registry,
// whereas the underlying `rpm-ostree compose image` defaults to
// an ociarchive.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/coreos/coreos-assembler/internal/pkg/cmdrun"
	"github.com/coreos/coreos-assembler/internal/pkg/cosash"
	"github.com/spf13/cobra"
)

const initConfigPath = "src/config.json"
const defaultManifest = "src/config/manifest.yaml"

type BuildCImageOptions struct {
	authfile   string
	initialize bool
}

var (
	BuildCImageOpts BuildCImageOptions

	cmdBuildCImage = &cobra.Command{
		Use:   "build-cimage",
		Short: "cosa build-cimage [repository]",
		Args:  cobra.ExactArgs(1),
		Long:  "Initialize directory for ostree container image build",
		RunE:  implRunBuildCImage,
	}
)

func init() {
	cmdBuildCImage.Flags().BoolVarP(
		&BuildCImageOpts.initialize, "initialize", "i", false,
		"Assume target image does not exist")
	cmdBuildCImage.Flags().StringVar(
		&BuildCImageOpts.authfile, "authfile", "",
		"Path to container authentication file")
}

func runBuildCImage(argv []string) error {
	cmdBuildCImage.SetArgs(argv)
	return cmdBuildCImage.Execute()
}

// This is a Go reipmlementation of pick_yaml_or_else_json() from cmdlib.sh
func pickConfigFileYamlOrJson(name string, preferJson bool) (string, error) {
	jsonPath := fmt.Sprintf("src/config/%s.json", name)
	yamlPath := fmt.Sprintf("src/config/%s.yaml", name)
	if _, err := os.Stat(jsonPath); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		jsonPath = ""
	}
	if _, err := os.Stat(yamlPath); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		yamlPath = ""
	}
	if jsonPath != "" && yamlPath != "" {
		return "", fmt.Errorf("found both %s and %s", jsonPath, yamlPath)
	}
	if jsonPath != "" {
		return jsonPath, nil
	}
	return yamlPath, nil
}

type configVariant struct {
	Variant string `json:"coreos-assembler.config-variant"`
}

func getVariant() (string, error) {
	contents, err := ioutil.ReadFile(initConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}

	var variantData configVariant
	if err := json.Unmarshal(contents, &variantData); err != nil {
		return "", fmt.Errorf("parsing %s: %w", initConfigPath, err)
	}

	return variantData.Variant, nil
}

func implRunBuildCImage(c *cobra.Command, args []string) error {
	if err := cmdrun.RunCmdSyncV("cosa", "build", "--prepare-only"); err != nil {
		return err
	}

	csh, err := cosash.NewCosaSh()
	if err != nil {
		return err
	}

	basearch, err := csh.BaseArch()
	if err != nil {
		return err
	}
	variant, err := getVariant()
	if err != nil {
		return err
	}
	manifest := defaultManifest
	if variant != "" {
		manifest = fmt.Sprintf("src/config/manifest-%s.yaml", variant)
	}

	repository := args[0]

	buildArgs := []string{"compose", "image", "--format", "registry", "--layer-repo", "tmp/repo"}
	if BuildCImageOpts.initialize {
		buildArgs = append(buildArgs, "--initialize")
	}
	if BuildCImageOpts.authfile != "" {
		buildArgs = append(buildArgs, "--authfile", BuildCImageOpts.authfile)
	}
	if _, err := os.Stat("tmp/cosa-transient"); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		cachedir := "cache/buildcimage-cache"
		if err := os.MkdirAll(cachedir, 0o755); err != nil {
			return err
		}
		buildArgs = append(buildArgs, "--cachedir", cachedir)
	}
	manifestLock, err := pickConfigFileYamlOrJson(fmt.Sprintf("manifest-lock.%s", basearch), true)
	if err != nil {
		return err
	}
	manifestLockOverrides, err := pickConfigFileYamlOrJson("manifest-lock.overrides", false)
	if err != nil {
		return err
	}
	manifestLockArchOverrides, err := pickConfigFileYamlOrJson(fmt.Sprintf("manifest-lock.overrides.%s", basearch), false)
	if err != nil {
		return err
	}
	for _, lock := range []string{manifestLock, manifestLockOverrides, manifestLockArchOverrides} {
		if lock != "" {
			buildArgs = append(buildArgs, "--lockfile", lock)
		}
	}
	buildArgs = append(buildArgs, manifest)
	buildArgs = append(buildArgs, repository)

	argv0 := "rpm-ostree"
	priv, err := csh.HasPrivileges()
	if err != nil {
		return err
	}
	if priv {
		argv0 = "sudo"
		buildArgs = append([]string{"rpm-ostree"}, buildArgs...)
	} else {
		return fmt.Errorf("this command currently requires the ability to create nested containers")
	}

	if err := cmdrun.RunCmdSyncV(argv0, buildArgs...); err != nil {
		return err
	}

	return nil
}
