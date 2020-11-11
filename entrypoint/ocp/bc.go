/*
	Main interface into OCP Build targets.

	This supports running via:
	- generic Pod with a Service Account
	- an OpenShift buildConfig

*/

package ocp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/coreos/entrypoint/cosa"
	"github.com/coreos/entrypoint/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
)

var (
	// srvBucket is the name of the bucket to use for remote
	// files being served up
	srvBucket = "source"

	// buildConfigs is a builder.
	_ = Builder(&buildConfig{})
)

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
}

// newBC accepts a context and returns a buildConfig
func newBC() (*buildConfig, error) {

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

	// Init the API Client for k8s itself
	// The API client requires that the pod/buildconfig include a service account.
	k8sAPIErr := k8sAPIClient()
	if k8sAPIErr != nil && k8sAPIErr != ErrNotInCluster {
		log.Errorf("Failed to initalized Kubernetes in cluster API client: %v", k8sAPIErr)
		return nil, k8sAPIErr
	}

	// Init the OpenShift Build API Client.
	buildAPIErr := ocpBuildClient()
	if buildAPIErr != nil && buildAPIErr != ErrNoOCPBuildSpec {
		log.Errorf("Failed to initalized the OpenShift Build API Client: %v", buildAPIErr)
		return nil, buildAPIErr
	}

	// Query Kubernetes to find out what this pods network identity is.
	// TODO: remove this CI exception once we have a kubernetes mock
	if !forceNotInCluster {
		v.HostPod = fmt.Sprintf("%s-%s-build",
			apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
			apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
		)

		_, ok := apiBuild.Annotations[ciRunnerTag]
		if ok {
			v.HostIP = apiBuild.Annotations[fmt.Sprintf(ciAnnotation, "IP")]
		} else {
			log.Info("Querying for pod ID")
			hIP, err := getPodIP(v.HostPod)
			if err != nil {
				log.Errorf("Failed to determine buildconfig's pod")
			}
			v.HostIP = hIP
		}

		log.WithFields(log.Fields{
			"buildconfig/name":   apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
			"buildconfig/number": apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
			"podname":            v.HostPod,
			"podIP":              v.HostIP,
		}).Info("found build.openshift.io/buildconfig identity")
	}

	// Build requires either a Build API Client or Kuberneres Cluster client.
	if k8sAPIErr != nil && buildAPIErr != nil {
		return nil, ErrInvalidOCPMode
	}

	if _, err := os.Stat(cosaSrvDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("Context dir %q does not exist", cosaSrvDir)
	}

	if err := os.Chdir(cosaSrvDir); err != nil {
		return nil, fmt.Errorf("Failed to switch to context dir: %s: %v", cosaSrvDir, err)
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
	return &v, nil
}

// Exec executes the command using the closure for the commands
func (bc *buildConfig) Exec(ctx context.Context) error {
	curD, _ := os.Getwd()
	defer func(c string) { _ = os.Chdir(c) }(curD)

	if err := os.Chdir(cosaSrvDir); err != nil {
		return err
	}

	// Define, but do not start minio.
	m := newMinioServer()
	m.dir = cosaSrvDir
	m.Host = bc.HostIP

	// returnTo informs the workers where to send their bits
	returnTo := &Return{
		Minio:  m,
		Bucket: "builds",
	}

	// Prepare the remote files.
	var remoteFiles []*RemoteFile
	r, err := bc.ocpBinaryInput(m)
	if err != nil {
		return fmt.Errorf("failed to process binary input: %w", err)
	}
	remoteFiles = append(remoteFiles, r...)

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
	if err := m.start(ctx); err != nil {
		return fmt.Errorf("failed to start Minio: %w", err)
	}

	if err := m.ensureBucketExists(ctx, "builds"); err != nil {
		return err
	}

	// Determine what stages happen in what pod number.
	stageCmdIDs := make(map[int][]string)
	c := 0
	for _, s := range bc.JobSpec.Stages {
		if c == 0 {
			stageCmdIDs[0] = []string{s.ID}
			continue
		}
		if s.OwnPod && len(stageCmdIDs[c]) != 0 {
			c++
			log.Infof("Stage %q will be executed in its own pod", s.ID)
		}
		stageCmdIDs[c] = append(stageCmdIDs[c], s.ID)
		log.Infof("Stage %q assigned to pod %d", s.ID, c)
	}

	buildID := ""

	log.Infof("Job will be run over %d pods", len(stageCmdIDs))
	for n, s := range stageCmdIDs {
		ws := &workSpec{
			APIBuild:      apiBuild,
			ExecuteStages: s,
			JobSpec:       bc.JobSpec,
			RemoteFiles:   remoteFiles,
			Return:        returnTo,
		}

		// For _each_ stage, we need to check if a meta.json exists.
		// mBuild - *cosa.Build representing meta.json
		// buildPath - location of the build artifacts
		// mErr - error or nil
		mBuild, mPath, mErr := cosa.ReadBuild(cosaSrvDir, buildID, "")
		artifactPath := filepath.Dir(mPath)

		// The buildID may have been updated by worker pod.
		// Log the fact for propserity.
		if mBuild != nil && mBuild.BuildID != buildID {
			log.WithField("buildID", mBuild.BuildID).Info("Found new build ID")
		}

		// Include the base builds.json and meta.json.
		if buildID != "" {
			mPath := filepath.Join(buildID, cosa.CosaMetaJSON)
			for _, k := range []string{mPath, cosa.CosaBuildsJSON} {
				ws.RemoteFiles = append(ws.RemoteFiles, &RemoteFile{
					Bucket: "builds",
					Minio:  m,
					Object: k,
				})
			}
		}

		// Iterate over the stages and figure out what the required files are
		for _, sID := range s {
			S, _ := bc.JobSpec.GetStage(sID)
			if len(S.RequireArtifacts) > 0 && mErr != nil {
				return fmt.Errorf("stage %s requires artifacts %v but meta.json not found: %v",
					sID, S.RequireArtifacts, err)
			}

			for _, artifact := range S.RequireArtifacts {
				bArtifact, err := mBuild.GetArtifact(artifact)
				if err != nil {
					return fmt.Errorf("found to find artifact %s: %w", artifact, err)
				}
				keyPath := filepath.Join(artifactPath, bArtifact.Path)
				keyPath = strings.Replace(keyPath, "", filepath.Join(cosaSrvDir, "builds"), 1)

				log.WithFields(log.Fields{
					"stage":         sID,
					"artifact":      artifact,
					"artifact path": keyPath,
					"buildID":       buildID,
				}).Info("required artifact")

				r := &RemoteFile{
					Artifact: bArtifact,
					Bucket:   "builds",
					Minio:    m,
					Object:   keyPath,
				}
				ws.RemoteFiles = append(ws.RemoteFiles, r)
			}
		}
		eVars, err := ws.getEnvVars()
		if err != nil {
			return err
		}

		index := n + 1
		if err := createWorkerPod(ctx, index, eVars); err != nil {
			log.Errorf("FAILED stage: %v", err)
		}
	}

	// Yeah, this is lazy...
	args := []string{"find", "/srv", "-type", "f"}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

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
		log.Info("Binary input will be served to remote mos.")
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
