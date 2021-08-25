package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coreos/coreos-assembler-schema/cosa"

	log "github.com/sirupsen/logrus"
)

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
	ID                  string `yaml:"id,omitempty" json:"id,omitempty"`
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
	RequireArtifacts []string `yaml:"require_artifacts,flow,omitempty" json:"require_artifacts,omitempty"`

	// RequestArtifacts are files that are provided if they are there. Examples include
	// 'caches' for `/srv/cache` and `/srv/tmp/repo` tarballs or `ostree` which are really useful
	// for base builds.
	RequestArtifacts []string `yaml:"request_artifacts,flow,omitempty" json:"request_artifacts,omitempty"`

	// BuildArtifacts produces "known" artifacts. The special "base"
	// will produce an OSTree and QCOWs.
	BuildArtifacts []string `yaml:"build_artifacts,flow,omitempty" json:"build_artifacts,omitempty"`

	// Commands are arbitrary commands run after an Artifact builds.
	// Instead of running `cosa buildextend-?` as a command, its preferrable
	// use the bare name in BuildArtifact.
	Commands []string `yaml:"commands,flow,omitempty" json:"commands,omitempty"`

	// PublishArtifacts will upload defined BuildArtifacts to the cloud providers
	PublishArtifacts []string `yaml:"publish_artifacts,omitempty" json:"publish_artifacts,omitempty"`

	// PrepCommands are run before Artifact builds, while
	// PostCommands are run after. Prep and Post Commands are run serially.
	PrepCommands []string `yaml:"prep_commands,flow,omitempty" json:"prep_commands,omitempty"`
	PostCommands []string `yaml:"post_commands,flow,omitempty" json:"post_commands,omitempty"`

	// PostAlways ensures that the PostCommands are always run.
	PostAlways bool `yaml:"post_always,omitempty" json:"post_always,omitempty"`

	// ExecutionOrder is a number value that defines the order of stages. If two stages
	// share the same execution order number, then they are allowed to run concurrently to each other.
	ExecutionOrder int `yaml:"execution_order,omitempty" json:"execution_order,omitempty"`

	// ReturnCache returns a tarball of `/srv/cache`, while RequireCahce ensures the tarball
	// is fetched unpacked into `/srv/cahce`. RequestCache is a non-blocking, optional versopn
	// of RequireCache.
	ReturnCache  bool `yaml:"return_cache,omitempty" json:"return_cache,omitempty"`
	RequireCache bool `yaml:"require_cache,omitempty" json:"require_cache_repo,omitempty"`
	RequestCache bool `yaml:"request_cache,omitempty" json:"reqest_cache_repo,omitempty"`

	// ReturnCacheRepo returns a tarball of `/srv/repo`, while RequireCacheRepo ensures the
	// tarball is fetched and unpacked into `/srv/repo`. RequestCacheRepo is a non-blocking, optional
	// version of RequireCacheRepo
	ReturnCacheRepo  bool `yaml:"return_cache_repo,omitempty" json:"return_cache_repo,omitempty"`
	RequireCacheRepo bool `yaml:"require_cache_repo,omitempty" json:"require_cache_repo_repo,omitempty"`
	RequestCacheRepo bool `yaml:"request_cache_repo,omitempty" json:"request_cache_repo_repo,omitempty"`

	// ReturnFiles returns a list of files that were requested to be returned.
	ReturnFiles []string `yaml:"return_files,omitempty" json:"return_files,omitempty"`

	// KolaTests are shorthands for testing.
	KolaTests []string `yaml:"kola_tests,omitempty" json:"kola_tests,omitempty"`

	// Overrides is a list of Overrides to apply to the OS tree
	Overrides []Override `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

// These are the only hard-coded commands that Gangplank understand.
const (
	// defaultBaseCommand is the basic build command
	defaultBaseCommand = "cosa fetch; cosa build %s;"
	// defaultBaseDelayMergeCommand is used for distributed build using
	// parallel workers pods.
	defaultBaseDelayMergeCommand = "cosa fetch; cosa build %s --delay-meta-merge;"

	// defaultFinalizeComamnd ensures that the meta.json is merged.
	defaultFinalizeCommand = "cosa meta --finalize;"
)

// cosaBuildCmds checks if b is a buildable artifact type and then
// returns it.
func cosaBuildCmd(b string, js *JobSpec) ([]string, error) {
	log.WithField("command", b).Info("checking shorthand")
	switch v := strings.ToLower(b); v {
	case "base", "ostree", "qemu":
		if v == "base" {
			v = ""
		}
		if js.DelayedMetaMerge {
			return []string{fmt.Sprintf(defaultBaseDelayMergeCommand, v)}, nil
		}
		return []string{fmt.Sprintf(defaultBaseCommand, v)}, nil
	case "finalize":
		return []string{defaultFinalizeCommand}, nil
	case "live":
		return []string{fmt.Sprintf("cosa buildextend-%s", b)}, nil
	}

	if cosa.CanArtifact(b) {
		return []string{fmt.Sprintf("cosa buildextend-%s", b)}, nil
	}
	return nil, fmt.Errorf("%s is not a known buildable artifact", b)
}

// getCommands renders the automatic artifacts and publication commands
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
	return ret, nil
}

// getPostCommands generates the post commands from a synthatis of pre-defined
// post commands, kola tests and the cloud publication steps.
func (s *Stage) getPostCommands(rd *RenderData) ([]string, error) {
	ret := s.PostCommands

	log.WithField("mapping tests", s.KolaTests).Infof("Resolving test definitions")
	for _, kolaTest := range s.KolaTests {
		tk, ok := kolaTestDefinitions[kolaTest]
		if !ok {
			return nil, fmt.Errorf("test %q is an unknown short hand", kolaTest)
		}
		ret = append(ret, tk.PostCommands...)
	}

	pc, err := s.getPublishCommands(rd)
	if err != nil {
		return nil, err
	}

	ret = append(ret, pc...)
	return ret, nil
}

// getPublishCommands returns the cloud publication commands.
func (s *Stage) getPublishCommands(rd *RenderData) ([]string, error) {
	var publishCommands []string
	c := rd.JobSpec.CloudsCfgs
	for _, cloud := range s.PublishArtifacts {
		if !cosa.CanArtifact(cloud) {
			return nil, fmt.Errorf("Invalid cloud artifact: %v", cloud)
		}

		config, err := c.GetCloudCfg(cloud)
		if err != nil {
			return nil, err
		}

		pc, err := config.GetPublishCommand(rd.Meta.BuildID)
		if err != nil {
			return nil, err
		}
		publishCommands = append(publishCommands, pc)
	}

	return publishCommands, nil
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

	postCommands, err := s.getPostCommands(rd)
	if err != nil {
		log.WithError(err).Error("failed to get post commands")
		return err
	}

	if len(s.PrepCommands) == 0 && len(cmds) == 0 && len(postCommands) == 0 {
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
	if err := ioutil.WriteFile(postScript, []byte(strings.Join(postCommands, "\n")), 0755); err != nil {
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
		// Don't use `range scripts` here because the map is unordered
		// and we want to execute the commands in order. We know the map
		// was populated in order with index[i] so just use the length
		// here and count from 0 to len(scripts).
		for i := 0; i < len(scripts); i++ {
			if err := rd.RendererExecuter(ctx, envVars, scripts[i]); err != nil {
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
	pseudoStages = []string{"base", "finalize", "live"}
	// buildableArtifacts are known artifacts types from the schema.
	buildableArtifacts = append(pseudoStages, cosa.GetCommandBuildableArtifacts()...)

	// baseArtifacts are default built by the "base" short-hand
	baseArtifacts = []string{"ostree", "qemu"}
)

// isBaseArtifact is a check function for determining if an artifact
// is built by the base stage.
func isBaseArtifact(artifact string) bool {
	for _, k := range baseArtifacts {
		if k == artifact {
			return true
		}
	}
	return false
}

// GetArtifactShortHandNames returns shorthands for buildable stages
func GetArtifactShortHandNames() []string {
	return buildableArtifacts
}

// addShorthandToStage adds in a build shorthand into the stage and
// ensures that required dependencies are correclty ordered
// Ordering assumptions:
//  1. Base builds
//  2. Basic Kola Tests
//  3. Metal and Live ISO images
//  4. Metal and Live ISO testings
//  5. Cloud stages
func addShorthandToStage(artifact string, stage *Stage) {

	quickStage := func(noun string) *Stage {
		switch noun {
		case "base":
			return &Stage{
				BuildArtifacts:   []string{"base"},
				ExecutionOrder:   1,
				RequestArtifacts: []string{"ostree"},
				RequestCache:     true,
				RequestCacheRepo: true,
			}
		case "extensions":
			return &Stage{
				BuildArtifacts:   []string{"extensions"},
				ExecutionOrder:   2,
				RequireArtifacts: []string{"ostree"},
				RequireCache:     true,
				RequireCacheRepo: true,
			}
		case "finalize":
			return &Stage{
				BuildArtifacts: []string{"finalize"},
				ExecutionOrder: 999,
			}
		case "live":
			return &Stage{
				ExecutionOrder:   2,
				BuildArtifacts:   []string{"live"},
				RequireArtifacts: []string{"ostree", "metal", "metal4k"},
			}
		case "metal":
			return &Stage{
				ExecutionOrder:   3,
				BuildArtifacts:   []string{"metal"},
				RequireArtifacts: []string{"ostree"},
			}
		case "metal4k":
			return &Stage{
				ExecutionOrder:   3,
				BuildArtifacts:   []string{"metal4k"},
				RequireArtifacts: []string{"ostree"},
			}
		case "oscontainer":
			return &Stage{
				BuildArtifacts:   []string{"oscontainer"},
				ExecutionOrder:   2,
				RequireArtifacts: []string{"ostree"},
				RequireCache:     true,
				RequireCacheRepo: true,
			}
		default:
			// check if the short hand is a test stage
			testStage, ok := kolaTestDefinitions[noun]
			if ok {
				return &testStage
			}
			// otherwise its likely a cloud stage
			if !cosa.CanArtifact(artifact) {
				break
			}
			return &Stage{
				ExecutionOrder:   5,
				BuildArtifacts:   []string{artifact},
				RequireArtifacts: []string{"qemu"},
			}
		}
		log.WithField("artifact", noun).Fatalf("unknown artifact type")
		return nil
	}

	working := quickStage(artifact)

	// remove is helper for removing the first matching item from a slice
	remove := func(slice []string, key string) ([]string, bool) {
		for x := 0; x < len(slice); x++ {
			if slice[x] == key {
				return append(slice[:x], slice[x+1:]...), true
			}
		}
		return slice, false
	}

	unique := func(strSlice []string) []string {
		keys := make(map[string]bool)
		list := []string{}
		for _, entry := range strSlice {
			if _, value := keys[entry]; !value {
				keys[entry] = true
				list = append(list, entry)
			}
		}
		return list
	}

	// if the stage returns cache/repo cache then it provides the requires
	if working.RequireCache && !stage.ReturnCache {
		stage.RequireCache = true
		stage.RequestCache = false
	}
	if working.RequireCacheRepo && !stage.ReturnCacheRepo {
		stage.RequireCacheRepo = true
		stage.RequestCacheRepo = false
	}

	// Handle the return/requires for cache and repo cache
	if working.ReturnCache {
		stage.ReturnCache = working.ReturnCache
	}
	if working.ReturnCacheRepo {
		stage.ReturnCacheRepo = working.ReturnCacheRepo
	}

	// Only set RequestCache[Repo] we don't require them.
	if working.RequestCache && (!stage.RequireCache || !working.RequireCache) {
		stage.RequestCache = true
	}
	if working.RequestCacheRepo && (!stage.RequireCacheRepo || !working.RequireCacheRepo) {
		stage.RequestCacheRepo = true
	}

	// if the stage returns cache/repo cache then it provides the requires
	if working.RequireCache && !stage.ReturnCache {
		stage.RequireCache = true
	}
	if working.RequireCacheRepo && !stage.ReturnCacheRepo {
		stage.RequireCacheRepo = true
	}

	// Add the commands if defined
	stage.Commands = append(stage.Commands, working.Commands...)
	stage.PrepCommands = append(stage.PrepCommands, working.PrepCommands...)
	stage.PostCommands = append(stage.PostCommands, working.PostCommands...)

	stage.RequestArtifacts = append(stage.RequestArtifacts, working.RequestArtifacts...)
	stage.BuildArtifacts = append(stage.BuildArtifacts, working.BuildArtifacts...)
	stage.RequireArtifacts = append(stage.RequireArtifacts, working.RequireArtifacts...)

	// Assume the lowest stage execution order
	if working.ExecutionOrder < stage.ExecutionOrder || stage.ExecutionOrder == 0 {
		stage.ExecutionOrder = working.ExecutionOrder
	}

	randID := time.Now().UTC().UnixNano() // Ensure a random ID
	stage.ID = fmt.Sprintf("ExecOrder %d Stage %d", stage.ExecutionOrder, randID)
	stage.Description = fmt.Sprintf("Stage %d execution %s",
		stage.ExecutionOrder, strings.Join(append(stage.BuildArtifacts, stage.KolaTests...), ","))

	// Get the order that artifacts should be built
	artifactOrder := make(map[int][]string)
	for _, v := range stage.BuildArtifacts {
		if v == "caches" {
			stage.RequireCache = true
			stage.RequireCacheRepo = true
		} else {
			fakeStage := quickStage(v)
			artifactOrder[fakeStage.ExecutionOrder] = append(artifactOrder[fakeStage.ExecutionOrder], v)
		}
	}

	newOrder := []string{}
	for _, v := range artifactOrder {
		newOrder = append(newOrder, v...)
	}
	stage.BuildArtifacts = unique(newOrder)

	// Base implies building ostree and qemu
	buildArtifacts, buildsBase := remove(unique(newOrder), "base")
	if buildsBase {
		buildArtifacts, _ = remove(buildArtifacts, "ostree")
		buildArtifacts, _ = remove(buildArtifacts, "qemu")
		stage.BuildArtifacts = append([]string{"base"}, buildArtifacts...)
	}

	// If the synthetic stages requires/request optional artifact, but also builds it
	// then we need to remove it from the the requires.
	realRequires := stage.RequireArtifacts
	realOptional := stage.RequestArtifacts

	for _, ba := range stage.BuildArtifacts {
		for _, ra := range stage.RequireArtifacts {
			if ra == ba {
				realRequires, _ = remove(realRequires, ra)
			}
		}
		for _, oa := range stage.RequestArtifacts {
			if oa == ba {
				realOptional, _ = remove(realOptional, oa)
			}
		}
	}

	// base is short hand of ostree and qemu. Its handled specially
	// since we have to consider that "qemu"
	var foundBase bool
	realRequires, foundBase = remove(realRequires, "base")
	if foundBase || buildsBase {
		for _, v := range baseArtifacts {
			realRequires, _ = remove(realRequires, v)
			realOptional, _ = remove(realOptional, v)
		}
	}
	stage.RequireArtifacts = unique(realRequires)
	stage.RequestArtifacts = unique(realOptional)
}

// isValidArtifactShortHand checks if the shortand is valid
func isValidArtifactShortHand(a string) bool {
	valid := false
	for _, v := range strings.Split(strings.ToLower(a), "+") {
		if cosa.CanArtifact(v) {
			valid = true
		}
		for _, ps := range pseudoStages {
			if v == ps {
				valid = true
				break
			}
		}
	}
	return valid
}

// GenerateStages creates stages.
func (j *JobSpec) GenerateStages(fromNames, testNames []string, singleStage bool) error {
	j.DelayedMetaMerge = true
	j.Job.StrictMode = true

	for _, k := range fromNames {
		if !isValidArtifactShortHand(k) {
			return fmt.Errorf("artifact %s is an invalid artifact", k)
		}
	}
	for _, k := range testNames {
		if _, ok := kolaTestDefinitions[k]; !ok {
			return fmt.Errorf("kola test %s is an invalid kola name", k)
		}

	}

	if singleStage && len(fromNames) > 0 {
		newList := []string{strings.Join(append(fromNames, testNames...), "+")}
		fromNames = newList
	}

	for _, k := range append(fromNames, testNames...) {
		var s Stage
		for _, k := range strings.Split(k, "+") {
			addShorthandToStage(k, &s)
		}
		j.Stages = append(j.Stages, s)
	}

	return nil
}

// DeepCopy does a lazy deep copy by rendering the stage to JSON
// and then returning a new Stage defined by the JSON
func (s *Stage) DeepCopy() (Stage, error) {
	ns := Stage{}
	out, err := json.Marshal(s)
	if err != nil {
		return ns, err
	}
	err = json.Unmarshal(out, &ns)
	return ns, err
}

// addAllShortandsToStage adds all the shorthands
func addAllShorthandsToStage(stage *Stage, shorthands ...string) {
	for _, short := range shorthands {
		addShorthandToStage(short, stage)
	}
}
