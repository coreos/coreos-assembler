package spec

import (
	"os"
	"strings"

	"github.com/spf13/pflag"
)

/*
	cli.go supports creating inferred jobspecs.
*/

const (
	fedoraGitURL = "https://github.com/coreos/fedora-coreos-config"
	fedoraGitRef = "master"

	rhcosGitURL = "https://github.com/openshift/os"
	rhcosGitRef = "master"
)

// Default to building Fedora
var (
	gitRef = fedoraGitRef
	gitURL = fedoraGitURL
)

func init() {
	o, _ := os.LookupEnv("COSA_GANGPLANK_OS")
	if strings.ToLower(o) == "rhcos" {
		gitRef = rhcosGitRef
		gitURL = rhcosGitURL
	}
	if strings.ToLower(o) == "fcos" {
		gitRef = fedoraGitRef
		gitURL = fedoraGitURL
	}
}

// AddCliFlags returns the pflag set for use in the CLI.
func (js *JobSpec) AddCliFlags(cmd *pflag.FlagSet) {

	// Define the job definition
	cmd.StringVar(&js.Job.BuildName, "job-buildname", js.Job.BuildName, "job name to build")
	cmd.StringVar(&js.Job.VersionSuffix, "job-suffix", js.Job.VersionSuffix, "job suffix")
	cmd.BoolVar(&js.Job.IsProduction, "job-producution", js.Job.IsProduction, "job is a production job")

	// Default to building a fedora build
	if js.Recipe.GitRef == "" {
		js.Recipe.GitRef = gitRef
	}
	if js.Recipe.GitURL == "" {
		js.Recipe.GitURL = gitURL
	}

	// Define the recipe
	cmd.StringVar(&js.Recipe.GitRef, "git-ref", js.Recipe.GitRef, "Git ref for recipe")
	cmd.StringVar(&js.Recipe.GitURL, "git-url", js.Recipe.GitURL, "Git URL for recipe")

	// Push options
	cmd.StringVar(&js.Oscontainer.PushURL, "push-url", js.Oscontainer.PushURL, "push built images to location")
}
