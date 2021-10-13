package spec

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

/*
	cli.go supports creating inferred jobspecs.
*/

const (
	fedoraGitURL = "https://github.com/coreos/fedora-coreos-config"
	fedoraGitRef = "testing-devel"

	rhcosGitURL = "https://github.com/openshift/os"
	rhcosGitRef = "main"
)

// Default to building Fedora
var (
	gitRef = fedoraGitRef
	gitURL = fedoraGitURL

	// repos is a list a URLs that is added to the Repos.
	repos []string

	// copy-build is an extra build to copy build metadata for
	copyBuild string
)

func init() {
	r, ok := os.LookupEnv("COSA_YUM_REPOS")
	if ok {
		repos = strings.Split(r, ",")
	}

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

// strPtr is a helper for returning a string pointer
func strPtr(s string) *string { return &s }

// AddCliFlags returns the pflag set for use in the CLI.
func (js *JobSpec) AddCliFlags(cmd *pflag.FlagSet) {

	// Define the job definition
	cmd.StringVar(&js.Job.BuildName, "job-buildname", js.Job.BuildName, "job name to build")
	cmd.StringVar(&js.Job.VersionSuffix, "job-suffix", js.Job.VersionSuffix, "job suffix")
	cmd.BoolVar(&js.Job.IsProduction, "job-producution", js.Job.IsProduction, "job is a production job")
	cmd.StringSliceVar(&repos, "repo", repos, "yum repos to include for base builds")

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
	cmd.StringVar(&js.Recipe.GitCommit, "git-commit", "", "Optional Git commit to reset repo to for recipe")

	// Define any extra builds that we want to copy build metadata for
	cmd.StringVar(&copyBuild, "copy-build", "", "Optional: extra build to copy build metadata for")
}

// AddRepos adds an repositories from the CLI
func (js *JobSpec) AddRepos() {
	// Add in repositories
	for _, r := range repos {
		if r != "" {
			js.Recipe.Repos = append(
				js.Recipe.Repos,
				&Repo{
					URL: &r,
				})
		}
	}
}

// AddCopyBuild adds --copy-build from the CLI
func (js *JobSpec) AddCopyBuild() {
	if copyBuild != "" {
		log.Infof("Adding copy build meta for %s from the CLI", copyBuild)
		js.CopyBuild = copyBuild
	}
}

// AddCommands adds commands to a stage
func (s *Stage) AddCommands(args []string) {
	s.Commands = append(s.Commands, args...)
}

// AddReturnFiles adds return files to a stage
func (s *Stage) AddReturnFiles(args []string) {
	s.ReturnFiles = append(s.ReturnFiles, args...)
}

// AddRequires adds in requires based on the arifacts that a stage requires
// inconsideration of what the stage builds
func (s *Stage) AddRequires(args []string) {
	for _, req := range args {
		add := true
		for _, builds := range s.BuildArtifacts {
			if isBaseArtifact(req) {
				req = "base"
			}
			if builds == req {
				add = false
				break
			}
		}
		if add {
			s.RequireArtifacts = append(s.RequireArtifacts, req)
		}
	}
}
