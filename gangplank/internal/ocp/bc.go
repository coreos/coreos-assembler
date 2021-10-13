/*
	Main interface into OCP Build targets.

	This supports running via:
	- generic Pod with a Service Account
	- an OpenShift buildConfig

*/

package ocp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/coreos-assembler-schema/cosa"
	"github.com/coreos/gangplank/internal/spec"
	"github.com/minio/minio-go/v7"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
)

// srvBucket is the name of the bucket to use for remote
// files being served up
var srvBucket = "source"

// buildConfig is a builder.
var _ Builder = &buildConfig{}

// stageDependencyTimeOut is the length of time to wait for a stage's dependencies.
var stageDependencyTimeOut = 1 * time.Hour

func init() {
	buildJSONCodec = buildCodecFactory.LegacyCodec(buildapiv1.SchemeGroupVersion)
}

// buildConfig represent the input into a buildConfig.
type buildConfig struct {
	JobSpecURL  string `envVar:"COSA_JOBSPEC_URL"`
	JobSpecRef  string `envVar:"COSA_JOBSPEC_REF"`
	JobSpecFile string `envVar:"COSA_JOBSPEC_FILE"`
	CosaCmds    string `envVar:"COSA_CMDS"`

	// Information about the parent pod
	PodName      string `envVar:"COSA_POD_NAME"`
	PodIP        string `envVar:"COSA_POD_IP"`
	PodNameSpace string `envVar:"COSA_POD_NAMESPACE"`

	// HostIP is the kubernetes IP address of the running pod.
	HostIP  string
	HostPod string

	// Internal copy of the JobSpec
	JobSpec spec.JobSpec

	ClusterCtx ClusterContext
}

// newBC accepts a context and returns a buildConfig
func newBC(ctx context.Context, c *Cluster) (*buildConfig, error) {
	var v buildConfig
	rv := reflect.TypeOf(v)
	for i := 0; i < rv.NumField(); i++ {
		tag := rv.Field(i).Tag.Get(ocpStructTag)
		if tag == "" {
			continue
		}
		ev, found := os.LookupEnv(tag)
		if found {
			reflect.ValueOf(&v).Elem().Field(i).SetString(ev)
		}
	}

	// Init the OpenShift Build API Client.
	if err := ocpBuildClient(); err != nil {
		log.WithError(err).Error("Failed to initalized the OpenShift Build API Client")
		return nil, err
	}

	// Add the ClusterContext to the BuildConfig
	v.ClusterCtx = NewClusterContext(ctx, *c.toKubernetesCluster())
	ac, ns, kubeErr := GetClient(v.ClusterCtx)
	if kubeErr != nil {
		log.WithError(kubeErr).Info("Running without a cluster client")
	} else if ac != nil {
		v.HostPod = fmt.Sprintf("%s-%s-build",
			apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
			apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
		)

		log.Info("Querying for host IP")
		var e error
		v.HostIP, e = getPodIP(v.ClusterCtx, ac, ns, getHostname())
		if e != nil {
			log.WithError(e).Info("failed to query for hostname")
		}

		log.WithFields(log.Fields{
			"buildconfig/name":   apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
			"buildconfig/number": apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
			"podname":            v.HostPod,
			"podIP":              v.HostIP,
		}).Info("found build.openshift.io/buildconfig identity")
	}

	if _, err := os.Stat(cosaSrvDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("context dir %q does not exist", cosaSrvDir)
	}

	if err := os.Chdir(cosaSrvDir); err != nil {
		return nil, fmt.Errorf("failed to switch to context dir: %s: %v", cosaSrvDir, err)
	}

	// Locate the jobspec from local input OR from a remote repo.
	jsF := spec.DefaultJobSpecFile
	if v.JobSpecFile != "" {
		jsF = v.JobSpecFile
	}
	v.JobSpecFile = jsF
	jsF = filepath.Join(cosaSrvDir, jsF)
	js, err := spec.JobSpecFromFile(jsF)
	if err != nil {
		v.JobSpec = js
	} else {
		njs, err := spec.JobSpecFromRepo(v.JobSpecURL, v.JobSpecFile, filepath.Base(jsF))
		if err != nil {
			v.JobSpec = njs
		}
	}

	// Set default bucket if not defined
	if v.JobSpec.Minio.Bucket == "" && !v.JobSpec.Job.StrictMode {
		v.JobSpec.Minio.Bucket = "builder"
	}

	log.Info("Running Pod in buildconfig mode.")
	return &v, nil
}

// Exec executes the command using the closure for the commands
func (bc *buildConfig) Exec(ctx ClusterContext) (err error) {
	curD, _ := os.Getwd()
	defer func(c string) { _ = os.Chdir(c) }(curD)

	if err := os.Chdir(cosaSrvDir); err != nil {
		return err
	}

	// Define, but do not start minio.
	m := newMinioServer(bc.JobSpec.Minio.ConfigFile)
	m.dir = cosaSrvDir
	if !m.ExternalServer {
		if mf := getSshMinioForwarder(&bc.JobSpec); mf != nil {
			m.overSSH = mf
			m.Host = "127.0.0.1"
		}
	}

	// returnTo informs the workers where to send their bits
	returnTo := &Return{
		Minio:     m,
		Bucket:    bc.JobSpec.Minio.Bucket,
		KeyPrefix: bc.JobSpec.Minio.KeyPrefix,
	}

	// Prepare the remote files.
	var remoteFiles []*RemoteFile
	r, err := bc.ocpBinaryInput(m)
	if err != nil {
		return fmt.Errorf("failed to process binary input: %w", err)
	}
	remoteFiles = append(remoteFiles, r...)
	defer func() { _ = os.RemoveAll(filepath.Join(cosaSrvDir, sourceSubPath)) }()

	// Discover the stages and render each command into a script.
	r, err = bc.discoverStages(m)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}
	remoteFiles = append(remoteFiles, r...)

	if len(bc.JobSpec.Stages) == 0 {
		log.Info(`
No work to do. Please define one of the following:
	- 'COSA_CMDS' envVar with the commands to execute
	- Jobspec stages in your JobSpec file
	- Provide files ending in .cosa.sh

File can be provided in the Git Tree or by the OpenShift
binary build interface.`)
		return nil
	}

	// Start minio after all the setup. Each directory is an implicit
	// bucket and files, are implicit keys.
	//
	//  Job Control:
	//  	terminate channel: uses to tell workFunctions to ceases
	//  	errorCh channel: workFunctions report errors through this channel
	//  		when a error is recieved over the channel, a terminate is signaled
	//  	sig channel: this watches for sigterm and interrupts, which will
	//  		signal a terminate. (i.e. sigterm or crtl-c)
	//
	//  	The go-routine will run until it recieves a terminate itself.
	//
	errorCh := make(chan error)
	terminate := make(chan bool)
	if m.overSSH == nil {
		if err := m.start(ctx); err != nil {
			return fmt.Errorf("failed to start Minio: %w", err)
		}
	} else {
		if err := m.startMinioAndForwardOverSSH(ctx, terminate, errorCh); err != nil {
			return fmt.Errorf("failed to start Minio: %w", err)
		}
	}
	defer m.Kill()

	// Set the cosa builds IO Backend to minio.
	mc, err := m.client()
	if err != nil {
		return fmt.Errorf("failed to get minio client")
	}
	if err := m.ensureBucketExists(ctx, bc.JobSpec.Minio.Bucket); err != nil {
		return fmt.Errorf("failed to ensure '%s' bucket exists: %v", bc.JobSpec.Minio.Bucket, err)
	}

	// Set the cosa backend to minio. This allows for Gangplank to use
	// either a local directory (via minio) or remote object store (AWS or minio)
	// as the artifact store.
	if err := cosa.SetIOBackendMinio(ctx, mc, bc.JobSpec.Minio.Bucket, bc.JobSpec.Minio.KeyPrefix); err != nil {
		return err
	}

	// Find the last build, if any.
	lastBuild, _, err := cosa.ReadBuild("", "", cosa.BuilderArch())
	if err == nil {
		keyPath := filepath.Join(lastBuild.BuildID, cosa.BuilderArch())
		l := log.WithFields(log.Fields{
			"build": lastBuild.BuildID,
		})
		l.Info("found prior build")
		remoteFiles = append(
			remoteFiles,
			getBuildMeta(lastBuild.BuildID, keyPath, m, l, &bc.JobSpec.Minio)...,
		)

	} else {
		lastBuild = new(cosa.Build)
		log.Infof("no prior build found for arch: %s", cosa.BuilderArch())
	}

	// Copy any other builds requested by the user.
	if bc.JobSpec.CopyBuild != "" {
		copyBuild, _, err := cosa.ReadBuild("", bc.JobSpec.CopyBuild, cosa.BuilderArch())
		if err != nil {
			return fmt.Errorf("Failed to find build specified by CopyBuild: %v", bc.JobSpec.CopyBuild)
		}
		l := log.WithFields(log.Fields{
			"build": copyBuild.BuildID,
		})
		l.Info("copying requested build")
		keyPath := filepath.Join(copyBuild.BuildID, cosa.BuilderArch())
		remoteFiles = append(
			remoteFiles,
			getBuildMeta(copyBuild.BuildID, keyPath, m, l, &bc.JobSpec.Minio)...,
		)
	}

	// Dump the jobspec
	log.Infof("Using JobSpec definition:")
	if err := bc.JobSpec.WriteYAML(log.New().Out); err != nil {
		return err
	}

	// Create a cancelable context from the core context.
	podCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type workFunction func(terminate termChan) error
	workerFuncs := make(map[int][]workFunction)

	// Range over the stages and create workFunction, which is added to the
	// workerFuncs. Each workFunction is executed as a go routine that begins
	// work as soon as the `build_dependencies` are available.
	for idx, ss := range bc.JobSpec.Stages {

		// copy the stage to prevent corruption
		// using ss directly has proven to lead to memory corruptions (yikes!)
		s, err := ss.DeepCopy()
		if err != nil {
			return err
		}

		l := log.WithFields(log.Fields{
			"stage":             s.ID,
			"require_artifacts": s.RequireArtifacts,
		})

		cpod, err := NewCosaPodder(podCtx, apiBuild, idx)
		if err != nil {
			l.WithError(err).Error("Failed to create pod definition")
			return err
		}

		l.Info("Pod definition created")

		// ready spawns a go-routine that writes the return channel
		// when the stage's dependencies have been meet.
		ready := func(ws *workSpec, terminate <-chan bool) <-chan bool {
			out := make(chan bool)

			foundNewBuild := false
			buildID := lastBuild.BuildID

			// TODO: allow for selectable build id, instead of default
			//       to the latest build ID.
			go func(out chan<- bool) {
				check := func() bool {

					build, foundRemoteFiles, err := getStageFiles(buildID, l, m, lastBuild, &s, &bc.JobSpec.Minio)
					if build != nil && buildID != build.BuildID && !foundNewBuild {
						l.WithField("build ID", build.BuildID).Info("Using new buildID for lifetime of this build")
						buildID = build.BuildID
					}
					if err == nil {
						ws.RemoteFiles = append(remoteFiles, foundRemoteFiles...)
						out <- true
						return true
					}
					return false
				}

				for {
					if check() {
						l.Debug("all dependencies for stage have been meet")
						return
					}
					// Wait for the next check or terminate.
					select {
					case <-terminate:
						return
					case <-time.After(15 * time.Second):
						return
					}
				}
			}(out)

			return out
		}

		// anonFunc performs the actual work..
		anonFunc := func(terminate termChan) error {
			ws := &workSpec{
				APIBuild:      apiBuild,
				ExecuteStages: []string{s.ID},
				JobSpec:       bc.JobSpec,
				RemoteFiles:   remoteFiles,
				Return:        returnTo,
			}

			select {
			case <-terminate:
				return errors.New("terminate signal recieved, aborting stage")
			case <-time.After(stageDependencyTimeOut):
				return errors.New("required artifacts never appeared")
			case ok := <-ready(ws, terminate):
				if !ok {
					return fmt.Errorf("%s failed to become ready", s.ID)
				}

				l.Info("Worker dependences have been defined")
				eVars, err := ws.getEnvVars()
				if err != nil {
					return err
				}

				l.Info("Executing worker pod")
				if err := cpod.WorkerRunner(terminate, eVars); err != nil {
					return fmt.Errorf("%s failed: %w", s.ID, err)
				}
			}
			return nil
		}

		// If there is no default execution order, default to 2. The default
		// is due the short-hand defaults in stage.go that asssigns certain short-hands
		// to certain execution groups.
		eOrder := s.ExecutionOrder
		if eOrder == 0 {
			eOrder = 2
		}
		workerFuncs[eOrder] = append(workerFuncs[eOrder], anonFunc)
	}

	// Sort the ordering of the workerFuncs
	var order []int
	for key := range workerFuncs {
		order = append(order, key)
	}
	sort.Ints(order)

	// Watch the channels for signals to terminate
	errored := false
	go func() {
		sig := make(chan os.Signal, 256)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)

		term := func() {
			defer func() {
				recover() // nolint
			}()
			terminate <- true
		}

		for {
			select {
			case err, ok := <-errorCh:
				if err != nil {
					if err != nil {
						errored = true
						log.WithError(err).Error("Stage sent error")
						term()
					}
				}
				if !ok {
					return
				}
			case <-ctx.Done():
				log.Warning("Received cancellation")
				term()
			case s := <-sig:
				log.Warningf("Received signal %s", s)
				term()
			case die, ok := <-terminate:
				if !ok || die {
					log.Debug("Watch go-routine finished")
					return
				}
			}
			// Let the channel settle
			time.Sleep(1 * time.Second)
		}
	}()

	// For each execution group, launch all workers and wait for the group
	// to complete. If a workerFunc fails, then bail as soon as possible.
	for _, idx := range order {
		l := log.WithField("execution group", idx)
		wg := &sync.WaitGroup{}
		for _, v := range workerFuncs[idx] {
			wg.Add(1)
			go func(v workFunction) {
				defer func() {
					wg.Done()
					log.Debug("execution done")
				}()
				errorCh <- v(terminate)
			}(v)
		}
		wg.Wait()
		l.Debug("done with execution group")
	}

	close(terminate)

	if errored {
		return fmt.Errorf("process failed")
	}
	return nil
}

func copyFile(src, dest string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	destF, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer destF.Close()

	if _, err := io.Copy(destF, srcF); err != nil {
		return err
	}
	return err
}

// discoverStages supports the envVar and *.cosa.sh scripts as implied stages.
// The envVar stage will be run first, followed by the `*.cosa.sh` scripts.
func (bc *buildConfig) discoverStages(m *minioServer) ([]*RemoteFile, error) {
	var remoteFiles []*RemoteFile

	if bc.JobSpec.Job.StrictMode {
		log.Info("Job strict mode is set, skipping automated stage discovery.")
		return nil, nil
	}
	log.Info("Strict mode is off: envVars and *.cosa.sh files are implied stages.")

	sPrefix := "/bin/bash -xeu -o pipefail %s"
	// Add the envVar commands
	if bc.CosaCmds != "" {
		bc.JobSpec.Stages = append(
			bc.JobSpec.Stages,
			spec.Stage{
				Description: "envVar defined commands",
				DirectExec:  true,
				Commands: []string{
					fmt.Sprintf(sPrefix, bc.CosaCmds),
				},
				ID: "envVar",
			},
		)
	}

	// Add discovered *.cosa.sh scripts into a single stage.
	// *.cosa.sh scripts are all run on the same worker pod.
	scripts := []string{}
	foundScripts, _ := filepath.Glob("*.cosa.sh")
	for _, s := range foundScripts {
		dn := filepath.Base(s)
		destPath := filepath.Join(cosaSrvDir, srvBucket, dn)
		if err := copyFile(s, destPath); err != nil {
			return remoteFiles, err
		}

		// We _could_ embed the scripts directly into the jobspec's stage
		// but the jospec is embedded as a envVar. To avoid runing into the
		// 32K character limit and we have an object store running, we'll just use
		// that.
		remoteFiles = append(
			remoteFiles,
			&RemoteFile{
				Bucket: srvBucket,
				Object: dn,
				Minio:  m,
			},
		)

		// Add the script to the command interface.
		scripts = append(
			scripts,
			fmt.Sprintf(sPrefix, filepath.Join(cosaSrvDir, srvBucket, dn)),
		)
	}
	if len(scripts) > 0 {
		bc.JobSpec.Stages = append(
			bc.JobSpec.Stages,
			spec.Stage{
				Description: "*.cosa.sh scripts",
				DirectExec:  true,
				Commands:    scripts,
				ID:          "cosa.sh",
			},
		)
	}
	return remoteFiles, nil
}

// ocpBinaryInput decompresses the binary input. If the binary input is a tarball
// with an embedded JobSpec, its extracted, read and used.
func (bc *buildConfig) ocpBinaryInput(m *minioServer) ([]*RemoteFile, error) {
	var remoteFiles []*RemoteFile
	bin, err := recieveInputBinary()
	if err != nil {
		return nil, err
	}
	if bin == "" {
		return nil, nil
	}

	if strings.HasSuffix(bin, "source.bin") {
		f, err := os.Open(bin)
		if err != nil {
			return nil, err
		}

		if err := decompress(f, cosaSrvDir); err != nil {
			return nil, err
		}
		dir, key := filepath.Split(bin)
		bucket := filepath.Base(dir)
		r := &RemoteFile{
			Bucket:     bucket,
			Object:     key,
			Minio:      m,
			Compressed: true,
		}
		remoteFiles = append(remoteFiles, r)
		log.Info("Binary input will be served to remote workers")
	}

	// Look for a jobspec in the binary payload.
	jsFile := ""
	candidateSpec := filepath.Join(cosaSrvDir, bc.JobSpecFile)
	_, err = os.Stat(candidateSpec)
	if err == nil {
		log.Info("Found jobspec file in binary payload.")
		jsFile = candidateSpec
	}

	// Treat any yaml files as jobspec's.
	if strings.HasSuffix(apiBuild.Spec.Source.Binary.AsFile, "yaml") {
		jsFile = bin
	}

	// Load the JobSpecFile
	if jsFile != "" {
		log.WithField("jobspec", bin).Info("treating source as a jobspec")
		js, err := spec.JobSpecFromFile(jsFile)
		if err != nil {
			return nil, err
		}
		log.Info("Using OpenShift provided JobSpec")
		bc.JobSpec = js

		if bc.JobSpec.Recipe.GitURL != "" {
			log.Info("Jobpsec references a git repo -- ignoring buildconfig reference")
			apiBuild.Spec.Source.Git = new(buildapiv1.GitBuildSource)
			apiBuild.Spec.Source.Git.URI = bc.JobSpec.Recipe.GitURL
			apiBuild.Spec.Source.Git.Ref = bc.JobSpec.Recipe.GitRef
		}
	}
	return remoteFiles, nil
}

// getBuildMeta searches a path for all build meta files and creates remoteFiles
// for them. The keyPathBase is the relative path for the object.
func getBuildMeta(jsonPath, keyPathBase string, m *minioServer, l *log.Entry, mcfg *spec.Minio) []*RemoteFile {
	var metas []*RemoteFile

	mc, err := m.client()
	if err != nil {
		log.WithError(err).Warn("failed to get client")
		return nil
	}

	bucket, searchPath := getBucketObjectPath(mcfg, jsonPath)

	v := mc.ListObjects(context.Background(), bucket,
		minio.ListObjectsOptions{
			Recursive: true,
			Prefix:    searchPath,
		},
	)
	for {
		info, ok := <-v
		if !ok {
			break
		}
		if strings.HasSuffix(info.Key, "/") {
			continue
		}

		n := filepath.Base(info.Key)
		if !isKnownBuildMeta(n) {
			continue
		}

		metas = append(
			metas,
			&RemoteFile{
				Bucket:    bucket,
				Minio:     m,
				Object:    info.Key,
				ForcePath: filepath.Join("/srv/", "builds", getKeyLocalPath(mcfg, info.Key)),
			},
		)
		l.WithFields(log.Fields{
			"bucket": bucket,
			"key":    info.Key,
		}).Info("Included metadata")
	}

	bucket, obj := getBucketObjectPath(mcfg, "builds.json")
	if _, err := mc.StatObject(context.Background(), bucket, obj, minio.StatObjectOptions{}); err == nil {
		metas = append(
			metas,
			&RemoteFile{
				Minio:     m,
				Bucket:    bucket,
				Object:    obj,
				ForcePath: "/srv/builds/builds.json",
			},
		)
	}

	return metas
}

// getStageFiles returns the newest build and RemoteFiles for the stage.
// Depending on the stages dependencies, it will ensure that all meta-data
// and artifacts are send. If the stage requires/requests the caches,  it will be
// included in the RemoteFiles.
func getStageFiles(buildID string,
	l *log.Entry, m *minioServer, lastBuild *cosa.Build, s *spec.Stage, mcfg *spec.Minio) (*cosa.Build, []*RemoteFile, error) {
	var remoteFiles []*RemoteFile
	var keyPathBase string

	errMissingArtifactDependency := errors.New("missing an artifact depenedency")

	// For _each_ stage, we need to check if a meta.json exists.
	// mBuild - *cosa.Build representing meta.json
	mBuild, _, err := cosa.ReadBuild("", buildID, "")
	if err != nil {
		l.Info("No build history found")
	}

	// Handle {Require,Request}{Cache,CacheRepo}
	includeCache := func(tarball string, required, requested bool) error {
		if !required && !requested {
			return nil
		}

		bucket, obj := getBucketObjectPath(mcfg, "cache", tarball)
		cacheFound := m.Exists(bucket, obj)
		if !cacheFound {
			if required {
				l.WithField("cache", tarball).Debug("Does not exists yet")
				return errMissingArtifactDependency
			}
			return nil
		}

		remoteFiles = append(
			remoteFiles,
			&RemoteFile{
				Bucket:           bucket,
				Compressed:       true,
				ForceExtractPath: "/", // will extract to /srv/cache
				Minio:            m,
				Object:           obj,
			})
		return nil
	}
	if err := includeCache(cacheTarballName, s.RequireCache, s.RequestCache); err != nil {
		return nil, nil, errMissingArtifactDependency
	}
	if err := includeCache(cacheRepoTarballName, s.RequireCacheRepo, s.RequestCacheRepo); err != nil {
		return nil, nil, errMissingArtifactDependency
	}

	if mBuild != nil {
		// If the buildID is not known AND the worker finds a build ID,
		// then a new build has appeared.
		if buildID == "" {
			buildID = mBuild.BuildID
			l = log.WithField("buildID", buildID)
			log.WithField("buildID", mBuild.BuildID).Info("Found new build ID")
		}

		// base of the keys to fetch from minio "<buildid>/<arch>"
		keyPathBase = filepath.Join(buildID, cosa.BuilderArch())

		// Locate build meta data
		if lastBuild.BuildID != mBuild.BuildID {
			remoteFiles = append(
				remoteFiles,
				getBuildMeta(buildID, keyPathBase, m, l, mcfg)...,
			)
		}
	}

	// If no artfiacts are required we can skip checking for artifacts.
	if mBuild == nil {
		if len(s.RequireArtifacts) > 0 {
			l.Debug("Waiting for build to appear")
			return nil, nil, errMissingArtifactDependency
		}
	}

	// addArtifact is a helper function for adding artifacts
	addArtifact := func(artifact string) error {
		bArtifact, err := mBuild.GetArtifact(artifact)
		if err != nil {
			return errMissingArtifactDependency
		}

		// get the Minio relative path for the object
		// the full path needs to be broken in to <BUILDID>/<ARCH>/<FILE>
		bucket, key := getBucketObjectPath(mcfg, keyPathBase, filepath.Base(bArtifact.Path))

		// Check if the remote server has this
		if !m.Exists(bucket, key) {
			return errMissingArtifactDependency
		}

		r := &RemoteFile{
			Artifact:  bArtifact,
			Bucket:    bucket,
			Minio:     m,
			Object:    key,
			ForcePath: filepath.Join("/srv", "builds", getKeyLocalPath(mcfg, key)),
		}
		remoteFiles = append(remoteFiles, r)
		return nil
	}

	// Handle optional artifacts
	for _, artifact := range s.RequestArtifacts {
		if err = addArtifact(artifact); err != nil {
			l.WithField("artifact", artifact).Debug("skipping optional artifact")
		}
	}

	// Handle the required artifacts
	foundCount := 0
	for _, artifact := range s.RequireArtifacts {
		if err := addArtifact(artifact); err != nil {
			l.WithField("artifact", artifact).Warn("required artifact has not appeared yet")
			return mBuild, nil, errMissingArtifactDependency
		}
		foundCount++
	}

	if len(s.RequireArtifacts) != foundCount {
		return mBuild, nil, errMissingArtifactDependency
	}

	// Create a single tarball of from all the arbitrary overrides, then place in the cache directory.
	if len(s.Overrides) > 0 {
		overrideToken, _ := randomString(10)
		tmpD, err := ioutil.TempDir("", "override")
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(tmpD) //nolint

		for _, override := range s.Overrides {
			l.WithField("override", override.URI).Info("Processing Override")
			if err := override.Fetch(l, tmpD, decompress); err != nil {
				return nil, nil, fmt.Errorf("failed to write remote file: %v", err)
			}
		}

		bucket, key := getBucketObjectPath(mcfg, "cache", fmt.Sprintf("overrides-%s.tar.gz", overrideToken))
		if err := uploadPathAsTarBall(
			context.Background(), bucket, key, ".", tmpD, false,
			&Return{Minio: m}); err != nil {
			return nil, nil, err
		}
		remoteFiles = append(
			remoteFiles,
			&RemoteFile{
				Bucket:           mcfg.Bucket,
				Compressed:       true,
				ForceExtractPath: "/srv",
				Minio:            m,
				Object:           key,
			},
		)
	}

	for _, rf := range remoteFiles {
		l.WithFields(log.Fields{
			"bucket": rf.Bucket,
			"object": rf.Object,
		}).Debug("will request")
	}

	return mBuild, remoteFiles, nil

}

// getBucketObjectPath returns the bucket and the approriate path much like
// filepath.Join does, but for remote objects
func getBucketObjectPath(m *spec.Minio, parts ...string) (string, string) {
	path := filepath.Join(parts...)
	bucket := m.Bucket
	if m.KeyPrefix != "" {
		path = filepath.Join(m.KeyPrefix, path)
	}
	return bucket, path
}

// getKeyLocalPath strips off the
func getKeyLocalPath(m *spec.Minio, key string) string {
	return strings.TrimPrefix(key, m.KeyPrefix)
}
