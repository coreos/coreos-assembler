package spec

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coreos/gangplank/cosa"

	log "github.com/sirupsen/logrus"
)

// Stages describe the steps that a build should take.
type Stages struct {
	Stages []*Stage `yaml:"stage,omitempty"`
}

// GetStage returns the stage with the matching ID
func (j *JobSpec) GetStage(id string) (*Stage, error) {
	for _, stage := range j.Stages {
		if stage.ID == id {
			return &stage, nil
		}
	}
	return nil, fmt.Errorf("no such stage with ID %q", id)
}

// Stage is a single stage.
type Stage struct {
	ID                  string `yaml:"id"`
	Description         string `yaml:"description,omitempty" json:"description,omitempty"`
	ConcurrentExecution bool   `yaml:"concurrent,omitempty" json:"concurrent,omitempty"`

	// DirectExec signals that the command should not be written
	// to a file. Rather the command should directly executed.
	DirectExec bool `yaml:"direct_exec,omitempty" json:"direct_exec,omitempty"`

	// NotBlocking means that the stage does not block another stage
	// from starting execution (i.e. concurrent stage).
	NotBlocking bool `yaml:"not_blocking,omitempty" json:"not_blocking,omitempty"`

	// RequireArtifacts is a name of the required artifacts. If the
	// required artifact is missing (per the meta.json), the stage
	// will not be executed. RequireArticts _implies_ sending builds/builds.json
	// and builds/<BUILDID>/meta.json.
	// TODO: IMPLEMENT
	RequireArtifacts []string `yaml:"requires_artifacts,flow,omitempty" json:"requires_artifacts,omitempty"`

	// BuildArtifacts produces "known" artifacts. The special "base"
	// will produce an OSTree and QCOWs.
	BuildArtifacts []string `yaml:"build_artifacts,flow,omitempty" json:"build_artifacts,omitempty"`

	// Commands are arbitrary commands run after an Artifact builds.
	// Instead of running `cosa buildextend-?` as a command, its preferrable
	// use the bare name in BuildArtifact.
	Commands []string `yaml:"commands,flow,omitempty" json:"commands,omitempty"`

	// PrepCommands are run before Artifact builds, while
	// PostCommands are run after. Prep and Post Commands are run serially.
	PrepCommands []string `yaml:"prep_commands,flow,omitempty" json:"prep_commands,omitempty"`
	PostCommands []string `yaml:"post_commands,flow,omitempty" json:"post_commands,omitempty"`

	// PostAlways ensures that the PostCommands are always run.
	PostAlways bool `yaml:"post_always,omitempty" json:"post_always,omitempty"`
}

// These are the only hard-coded commands that Gangplank understand.
const (
	// defaultBaseCommand is the basic build command
	defaultBaseCommand = "cosa fetch; cosa build;"
	// defaultBaseDelayMergeCommand is used for distributed build using
	// parallel workers pods.
	defaultBaseDelayMergeCommand = "cosa fetch; cosa build --delay-meta-merge;"

	// defaultFinalizeComamnd ensures that the meta.json is merged.
	defaultFinalizeCommand = "cosa meta --finalize;"
)

// cosaBuildCmds checks if b is a buildable artifact type and then
// returns it.
func cosaBuildCmd(b string, js *JobSpec) ([]string, error) {
	log.WithField("command", b).Info("checking shorthand")
	switch strings.ToLower(b) {
	case "base":
		if js.DelayedMetaMerge {
			return []string{defaultBaseDelayMergeCommand}, nil
		}
		return []string{defaultBaseCommand}, nil
	case "finalize":
		return []string{defaultFinalizeCommand}, nil
	}

	if cosa.CanArtifact(b) {
		return []string{fmt.Sprintf("cosa buildextend-%s", b)}, nil
	}
	return nil, fmt.Errorf("%s is not a known buildable artifact", b)
}

// getCommands renders the automatic artifacts.
func (s *Stage) getCommands(rd *RenderData) ([]string, error) {
	if len(s.BuildArtifacts) > 0 {
		log.WithField("mapping artifacts", s.BuildArtifacts).Infof("Mapping artifacts")
	}
	numBuildArtifacts := len(s.BuildArtifacts)
	totalCmds := len(s.Commands) + numBuildArtifacts
	ret := make([]string, totalCmds)
	for i, ba := range s.BuildArtifacts {
		log.WithField("artifact", ba).Info("mapping artifact to command")
		cmds, err := cosaBuildCmd(ba, rd.JobSpec)
		if err != nil {
			log.WithError(err).Errorf("failed to map build artifacts: %v", ba)
			return nil, err
		}
		ret[i] = strings.Join(cmds, "\n")
	}
	for i, c := range s.Commands {
		ret[(numBuildArtifacts + i)] = c
	}
	fmt.Printf("%v", ret)
	return ret, nil
}

// Execute runs the commands of a stage.
func (s *Stage) Execute(ctx context.Context, rd *RenderData, envVars []string) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	if rd == nil {
		return errors.New("render data must not be nil")
	}

	log.Infof("Stage: %v", s)

	cmds, err := s.getCommands(rd)
	if err != nil {
		log.WithError(err).Error("failed to get stage commands")
		return err
	}
	if len(cmds) == 0 {
		return errors.New("no commands to execute")
	}
	log.WithField("cmd", cmds).Info("stage commands readied")

	tmpd, err := ioutil.TempDir("", "stages")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpd)

	// Render the pre and post scripts.
	prepScript := filepath.Join(tmpd, "prep.sh")
	if err := ioutil.WriteFile(prepScript, []byte(strings.Join(s.PrepCommands, "\n")), 0755); err != nil {
		return err
	}
	if err := rd.RendererExecuter(ctx, envVars, prepScript); err != nil {
		return fmt.Errorf("Failed execution of the prep stage: %w", err)
	}

	postScript := filepath.Join(tmpd, "post.sh")
	if err := ioutil.WriteFile(postScript, []byte(strings.Join(s.PostCommands, "\n")), 0755); err != nil {
		return err
	}
	if s.PostAlways {
		log.Info("PostCommand will be executed regardless of command success")
		defer func() {
			_ = rd.RendererExecuter(ctx, envVars, postScript)
		}()
	}

	// Write out each command to their own file. To enable concurrent execution.
	scripts := make(map[int]string)
	for i, c := range cmds {
		outf := filepath.Join(tmpd, fmt.Sprintf("script-%d.sh", i))
		if err := ioutil.WriteFile(outf, []byte(c), 0755); err != nil {
			return nil
		}
		scripts[i] = outf
		log.Infof("%s: %s", outf, c)
	}

	// Execute the main command stage.
	if !s.ConcurrentExecution {
		// Non-concurrent commands are run serially. Any failure will immediately
		// break the run.
		log.Infof("Executing %d stage commands serially", len(scripts))
		for _, v := range scripts {
			if err := rd.RendererExecuter(ctx, envVars, v); err != nil {
				return err
			}
		}
	} else {
		// Concurrent commands are run in parallel until all complete OR
		// one fails.
		log.Infof("Executing %d stage commands concurrently", len(scripts))
		wg := &sync.WaitGroup{}
		errors := make(chan error, len(scripts))
		for _, s := range scripts {
			wg.Add(1)
			go func(s string, w *sync.WaitGroup, ctx context.Context) {
				defer w.Done()
				log.Infof("STARTING command: %s", s)
				e := rd.RendererExecuter(ctx, envVars, s)
				errors <- e
				if err != nil {
					log.Infof("ERROR %s", s)
					return
				}
				log.Infof("SUCCESS %s", s)
			}(s, wg, ctx)
			// hack: ensure that scripts are started serially
			//       but may run concurrently
			time.Sleep(50 * time.Millisecond)
		}

		// Wait for the concurrent commands to run, and check
		// all errors to make sure non are swallowed.
		wg.Wait()
		var e error = nil
		for x := 0; x <= len(errors); x++ {
			err, ok := <-errors
			if !ok {
				break
			}
			if err != nil {
				log.Errorf("error recieved: %v", err)
				e = err
			}
		}
		if e != nil {
			return e
		}
	}

	// If PostAlways, then the postScript is executed in defer call above.
	if !s.PostAlways {
		return rd.RendererExecuter(ctx, envVars, postScript)
	}

	return nil
}

var (
	// pseudoStages are special setup and tear down phases.
	pseudoStages = []string{"base", "finalize"}
	// buildableArtifacts are known artifacts types from the schema.
	buildableArtifacts = append(pseudoStages, cosa.GetCommandBuildableArtifacts()...)
)

// GetArtifactShortHandNames returns shorthands for buildable stages
func GetArtifactShortHandNames() []string {
	return buildableArtifacts
}

// GenerateStages creates stages.
func (j *JobSpec) GenerateStages(fromNames []string) {
	if len(fromNames) == 0 {
		return
	}

	j.DelayedMetaMerge = true
	j.Job.StrictMode = true

	baseStage := Stage{
		ID:             "Generated Base Stage",
		BuildArtifacts: []string{"base"},
	}
	finalizeStage := Stage{
		ID:             "Generated Finalize Stage",
		BuildArtifacts: []string{"finalize"},
	}

	requireBase := false
	requireFinalize := false

	var stages []Stage
	var extra []Stage

	for _, k := range fromNames {
		switch k {
		case "base":
			requireBase = true
		case "finalize":
			requireFinalize = true
		case "metal", "metal4k":
			stages = append(stages,
				Stage{
					ID:               fmt.Sprintf("Generated %s build stage", k),
					BuildArtifacts:   []string{"metal", "metal4k"},
					RequireArtifacts: []string{"base"},
				})
		case "live-iso":
			stages = append(stages,
				Stage{
					ID:               "Generated Live-ISO stage",
					BuildArtifacts:   []string{"live-iso"},
					RequireArtifacts: []string{"qemu", "metal", "metal4k"},
				})
		default:
			extra = append(stages,
				Stage{
					ID:                  fmt.Sprintf("Generated %s stage", k),
					BuildArtifacts:      []string{k},
					RequireArtifacts:    []string{"qemu"},
					ConcurrentExecution: true,
				})
		}
	}

	appender := func(s ...Stage) {
		j.Stages = append(j.Stages, s...)
	}

	// base stage must be first
	if requireBase {
		appender(baseStage)
	}

	// add middle stages if any
	appender(stages...)
	appender(extra...)

	// finalize should happen after all other stages
	if len(j.Stages) > 0 || requireFinalize {
		appender(finalizeStage)
	}

}
