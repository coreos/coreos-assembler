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
	Description         string `yaml:"description,omitempty"`
	ConcurrentExecution bool   `yaml:"concurrent,omitempty"`

	// DirectExec signals that the command should not be written
	// to a file. Rather the command should directly executed.
	DirectExec bool `yaml:"direct_exec"`

	// OwnPod signals that the work should be done in a seperate pod.
	OwnPod bool `yaml:"own_pod,omitempty"`

	// NotBlocking means that the stage does not block another stage
	// from starting execution (i.e. concurrent stage). If true,
	// OwnPod should be true as well.
	NotBlocking bool `yaml:"blocking,omitempty"`

	// RequireArtifacts is a name of the required artifacts. If the
	// required artifact is missing (per the meta.json), the stage
	// will not be executed. RequireArticts _implies_ sending builds/builds.json
	// and builds/<BUILDID>/meta.json.
	// TODO: IMPLEMENT
	RequireArtifacts []string `yaml:"requires_artifacts,flow"`

	// BuildArtifacts produces "known" artifacts. The special "base"
	// will produce an OSTree and QCOWs.
	BuildArtifacts []string `yaml:"build_artifacts,flow"`

	// Commands are arbitrary commands run after an Artifact builds.
	// Instead of running `cosa buildextend-?` as a command, its preferrable
	// use the bare name in BuildArtifact.
	Commands []string `yaml:"commands,flow"`

	// PrepCommands are run before Artifact builds, while
	// PostCommands are run after. Prep and Post Commands are run serially.
	PrepCommands []string `yaml:"prep_commands,flow"`
	PostCommands []string `yaml:"post_commands,flow"`

	// PostAlways ensures that the PostCommands are always run.
	PostAlways bool `yaml:"post_always"`

	// Publication
	PublishArtifacts []*PublishArtifact `yaml:"publish_artifacts,flow"`
}

// cosaBuildCmds checks if b is a buildable artifact type and then
// returns it.
func cosaBuildCmd(b string) ([]string, error) {
	b = strings.ToLower(b)
	if b == "base" {
		return []string{"cosa fetch; cosa build;"}, nil
	}
	if cosa.CanArtifact(b) {
		return []string{fmt.Sprintf("cosa buildextend-%s", b)}, nil
	}
	return nil, fmt.Errorf("%s is not a known buildable artifact", b)
}

// getCommands renders the automatic artifacts.
//
func (s *Stage) getCommands() ([]string, error) {
	numBuildArtifacts := len(s.BuildArtifacts)
	totalCmds := len(s.Commands) + numBuildArtifacts
	ret := make([]string, totalCmds)
	for i, ba := range s.BuildArtifacts {
		cmds, err := cosaBuildCmd(ba)
		if err != nil {
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

func (s *Stage) getPublishArtifacts() []*PublishArtifact {
	return s.PublishArtifacts
}

// Execute runs the commands of a stage.
func (s *Stage) Execute(ctx context.Context, js *JobSpec, envVars []string) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	if js == nil {
		return errors.New("jobspec must not be nil")
	}

	cmds, err := s.getCommands()
	if err != nil {
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

	publishArtifacts := s.getPublishArtifacts()
	for _, pa := range publishArtifacts {
		envVars = append(envVars, pa.GetEnvVars()...)
		pc, err := pa.GetPublishCommand()
		if err != nil {
			return err
		}
		log.Infof("Adding command %s to stage", pc)
		cmds = append(cmds, pc)
	}

	// Render the pre and post scripts.
	prepScript := filepath.Join(tmpd, "prep.sh")
	if err := ioutil.WriteFile(prepScript, []byte(strings.Join(s.PrepCommands, "\n")), 0755); err != nil {
		return err
	}
	if err := js.RendererExecuter(ctx, envVars, prepScript); err != nil {
		return fmt.Errorf("Failed execution of the prep stage: %w", err)
	}

	postScript := filepath.Join(tmpd, "post.sh")
	if err := ioutil.WriteFile(postScript, []byte(strings.Join(s.PostCommands, "\n")), 0755); err != nil {
		return err
	}
	if s.PostAlways {
		log.Info("PostCommand will be executed regardless of command success")
		defer func() {
			_ = js.RendererExecuter(ctx, envVars, postScript)
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
			if err := js.RendererExecuter(ctx, envVars, v); err != nil {
				return err
			}
			mBuild, _, _ := cosa.ReadBuild("/srv", "", "")
			if mBuild.BuildID != "" {
				log.Infof("Setting environment variable COSA_BUILD=%s", mBuild.BuildID)
				envVars = append(envVars, fmt.Sprintf("COSA_BUILD=%s", mBuild.BuildID))

				for _, pa := range publishArtifacts {
					envVars = append(envVars, pa.GetEnvVars()...)
					pc, err := pa.GetPublishCommand()
					if err != nil {
						return err
					}
					log.Infof("Adding command %s to stage", pc)
					cmds = append(cmds, pc)
				}
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
				e := js.RendererExecuter(ctx, envVars, s)
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
		return js.RendererExecuter(ctx, envVars, postScript)
	}

	return nil
}
