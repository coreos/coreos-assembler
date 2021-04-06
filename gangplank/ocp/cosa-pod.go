package ocp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containers/libpod/libpod"
	"github.com/containers/libpod/libpod/define"
	"github.com/containers/libpod/pkg/bindings"
	"github.com/containers/libpod/pkg/bindings/containers"
	"github.com/containers/libpod/pkg/specgen"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/idtools"
	"github.com/opencontainers/runc/libcontainer/user"
	cspec "github.com/opencontainers/runtime-spec/specs-go"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	kvmLabel       = "devices.kubevirt.io/kvm"
	localPodEnvVar = "COSA_FORCE_NO_CLUSTER"
)

var (
	// volumes are the volumes used in all pods created
	volumes = []v1.Volume{
		{
			Name: "srv",
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{
					Medium: "",
				},
			},
		},
	}

	// volumeMounts are the common mounts used in all pods
	volumeMounts = []v1.VolumeMount{
		{
			Name:      "srv",
			MountPath: "/srv",
		},
	}

	// Define the Securite Contexts
	ocpSecContext = &v1.SecurityContext{}

	// On OpenShift 3.x, we require privileges.
	ocp3SecContext = &v1.SecurityContext{
		RunAsUser:  ptrInt(0),
		RunAsGroup: ptrInt(1000),
		Privileged: ptrBool(true),
	}

	// InitCommands to be run before work pod is executed.
	ocpInitCommand = []string{}

	// On OpenShift 3.x, /dev/kvm is unlikely to world RW. So we have to give ourselves
	// permission. Gangplank will run as root but `cosa` commands run as the builder
	// user. Note: on 4.x, gangplank will run unprivileged.
	ocp3InitCommand = []string{
		"/usr/bin/chmod 0666 /dev/kvm || echo missing kvm",
		"/usr/bin/stat /dev/kvm || :",
	}

	// Define the base requirements
	// cpu are in mils, memory is in mib
	baseCPU = *resource.NewQuantity(2, "")
	baseMem = *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI)

	ocp3Requirements = v1.ResourceList{
		v1.ResourceCPU:    baseCPU,
		v1.ResourceMemory: baseMem,
	}

	ocpRequirements = v1.ResourceList{
		v1.ResourceCPU:    baseCPU,
		v1.ResourceMemory: baseMem,
		kvmLabel:          *resource.NewQuantity(1, ""),
	}
)

// cosaPod is a COSA pod
type cosaPod struct {
	apiBuild   *buildapiv1.Build
	clusterCtx ClusterContext

	ocpInitCommand  []string
	ocpRequirements v1.ResourceList
	ocpSecContext   *v1.SecurityContext
	volumes         []v1.Volume
	volumeMounts    []v1.VolumeMount

	index     int
	pod       *v1.Pod
	terminate <-chan bool

	kubeResultCh       func() <-chan watch.Event
	kubeResultChCloser func()
}

// CosaPodder create COSA capable pods.
type CosaPodder interface {
	WorkerRunner(ctx ClusterContext, term <-chan bool, envVar []v1.EnvVar) error
}

// a cosaPod is a CosaPodder
var _ CosaPodder = &cosaPod{}

// NewCosaPodder creates a CosaPodder
func NewCosaPodder(
	ctx ClusterContext,
	apiBuild *buildapiv1.Build,
	index int) (CosaPodder, error) {

	cp := &cosaPod{
		apiBuild:   apiBuild,
		clusterCtx: ctx,
		index:      index,

		// Set defaults for OpenShift 4.x
		ocpRequirements: ocpRequirements,
		ocpSecContext:   ocpSecContext,
		ocpInitCommand:  ocpInitCommand,

		volumes:      volumes,
		volumeMounts: volumeMounts,
	}

	ac, _, err := GetClient(ctx)
	if err != nil {
		return nil, err
	}

	// If the builder is in-cluster (either as a BuildConfig or an unbound pod),
	// discover the version of OpenShift/Kubernetes.
	if ac != nil {
		vi, err := ac.DiscoveryClient.ServerVersion()
		if err != nil {
			return nil, fmt.Errorf("failed to query the kubernetes version: %w", err)
		}

		minor, err := strconv.Atoi(strings.TrimRight(vi.Minor, "+"))
		log.Infof("Kubernetes version of cluster is %s %s.%d", vi.String(), vi.Major, minor)
		if err != nil {
			return nil, fmt.Errorf("failed to detect OpenShift v4.x cluster version: %v", err)
		}
		// Hardcode the version for OpenShift 3.x.
		if minor == 11 {
			log.Infof("Creating container with OpenShift v3.x defaults")
			cp.ocpRequirements = ocp3Requirements
			cp.ocpSecContext = ocp3SecContext
			cp.ocpInitCommand = ocp3InitCommand
		}

		if err := cp.addVolumesFromSecretLabels(); err != nil {
			return nil, fmt.Errorf("failed to add secret volumes and mounts: %w", err)
		}
		if err := cp.addVolumesFromConfigMapLabels(); err != nil {
			return nil, fmt.Errorf("failed to add configMap volumes and mountsi: %w", err)
		}
	}

	return cp, nil
}

func ptrInt(i int64) *int64 { return &i }
func ptrBool(b bool) *bool  { return &b }

// getPodSpec returns a pod specification.
func (cp *cosaPod) getPodSpec(envVars []v1.EnvVar) *v1.Pod {
	podName := fmt.Sprintf("%s-%s-worker-%d",
		cp.apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
		cp.apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
		cp.index,
	)
	log.Infof("Creating pod %s", podName)

	cosaBasePod := v1.Container{
		Name:  podName,
		Image: apiBuild.Spec.Strategy.CustomStrategy.From.Name,
		Command: []string{
			"/usr/bin/dumb-init",
		},
		Args: []string{
			"/usr/bin/gangplank",
			"builder",
		},
		Env:             envVars,
		WorkingDir:      "/srv",
		VolumeMounts:    cp.volumeMounts,
		SecurityContext: cp.ocpSecContext,
		Resources: v1.ResourceRequirements{
			Limits:   cp.ocpRequirements,
			Requests: cp.ocpRequirements,
		},
	}

	cosaWork := []v1.Container{cosaBasePod}
	cosaInit := []v1.Container{}
	if len(cp.ocpInitCommand) > 0 {
		log.Infof("InitContainer has been defined")
		initCtr := cosaBasePod.DeepCopy()
		initCtr.Name = "init"
		initCtr.Args = []string{"/bin/bash", "-xc", fmt.Sprintf(`#!/bin/bash
export PATH=/usr/sbin:/usr/bin
%s
`, strings.Join(cp.ocpInitCommand, "\n"))}

		cosaInit = []v1.Container{*initCtr}
	}

	return &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,

			// Cargo-cult the labels comming from the parent.
			Labels: apiBuild.Labels,
		},
		Spec: v1.PodSpec{
			ActiveDeadlineSeconds:         ptrInt(1800),
			AutomountServiceAccountToken:  ptrBool(true),
			Containers:                    cosaWork,
			InitContainers:                cosaInit,
			RestartPolicy:                 v1.RestartPolicyNever,
			ServiceAccountName:            apiBuild.Spec.ServiceAccount,
			TerminationGracePeriodSeconds: ptrInt(300),
			Volumes:                       cp.volumes,
		},
	}
}

// WorkerRunner runs a worker pod on either OpenShift/Kubernetes or
// in as a podman container.
func (cp *cosaPod) WorkerRunner(ctx ClusterContext, term <-chan bool, envVars []v1.EnvVar) error {
	cluster, err := GetCluster(ctx)
	if err != nil {
		return err
	}
	if cluster.inCluster {
		return clusterRunner(ctx, term, cp, envVars)
	}
	return podmanRunner(ctx, term, cp, envVars)
}

// clusterRunner creates an OpenShift/Kubernetes pod for the work to be done.
// The output of the pod is streamed and captured on the console.
func clusterRunner(ctx ClusterContext, term <-chan bool, cp *cosaPod, envVars []v1.EnvVar) error {
	cs, ns, err := GetClient(cp.clusterCtx)
	if err != nil {
		return err
	}
	pod := cp.getPodSpec(envVars)

	ac := cs.CoreV1()
	resp, err := ac.Pods(ns).Create(pod)
	if err != nil {
		return fmt.Errorf("failed to create pod %s: %w", pod.Name, err)
	}
	log.Infof("Pod created: %s", pod.Name)
	cp.pod = pod

	status := resp.Status
	w, err := ac.Pods(ns).Watch(
		metav1.ListOptions{
			Watch:           true,
			ResourceVersion: resp.ResourceVersion,
			FieldSelector:   fields.Set{"metadata.name": pod.Name}.AsSelector().String(),
			LabelSelector:   labels.Everything().String(),
			TimeoutSeconds:  ptrInt(7200), // set a hard timeout to 2hrs
		},
	)
	if err != nil {
		return err
	}
	// expose the Watch to other routines.
	cp.kubeResultCh = w.ResultChan
	cp.kubeResultChCloser = w.Stop

	// open the terminate channel
	cp.terminate = term

	l := log.WithField("podname", pod.Name)

	defer func() {
		l.Debug("Requesting termination of remaining contianers")
		termOpts := &metav1.DeleteOptions{
			// the default grace period on OCP 3.x is 5min and OCP 4.x is 1min
			// If the pod is in an error state it will appear to be hang.
			GracePeriodSeconds: ptrInt(0),
		}
		if err := ac.Pods(ns).Delete(pod.Name, termOpts); err != nil {
			l.WithError(err).Error("Failed delete on pod, yolo.")
		}
		l.Debug("Pod work is returning to caller.")
	}()

	logStarted := false

	// Block waiting for the pod to finish or timeout.
	// HERE BE DEMOGORGRANS: in order for the termination logic to work, do
	//      not put any blocking calls in the select.
	for {
		select {
		// Watch the events channel to get the status of the pod.
		case events, ok := <-cp.kubeResultCh():
			if !ok {
				l.Error("failed waitching pod")
				return fmt.Errorf("orphaned pod")
			}

			resp = events.Object.(*v1.Pod)
			status = resp.Status

			l := log.WithFields(log.Fields{
				"podname":           pod.Name,
				"reason":            status.Reason,
				"phase":             status.Phase,
				"message":           status.Message,
				"containers status": status.ContainerStatuses,
			})

			// Check the _status_ of the pod
			switch sp := status.Phase; sp {
			case v1.PodRunning:
				if !logStarted {
					if err := cp.streamPodLogs(); err != nil {
						return err
					}
					logStarted = true
				}
			case v1.PodFailed, v1.PodUnknown, v1.PodReasonUnschedulable:
				l.Error("Pod failed")
				return fmt.Errorf("pod is a failure in its life")
			case v1.PodSucceeded:
				l.Infof("Pod successfully completed")
				return nil
			case v1.PodPending:
				l.Info("waiting...")
			default:
				l.Fatal("my logic has failed me")
			}

		// Ensure a dreadful and uncerimonious end to our job in case of
		// a timeout or a termination.
		case <-time.After(90 * time.Minute):
			return fmt.Errorf("pod %s did not complete work in time", pod.Name)
		case <-term:
			return fmt.Errorf("pod %s was signalled to terminate by main process", pod.Name)
		}
	}
}

// consoleLogWriter is an io.Writer that emits fancy logs to a screen.
type consoleLogWriter struct {
	startTime time.Time
	prefix    string
}

// consoleLogWriter is an io.Writer.
var _ io.Writer = &consoleLogWriter{}

// newConosleLogWriter is a helper function for getting a new writer.
func newConsoleLogWriter(prefix string) *consoleLogWriter {
	return &consoleLogWriter{
		prefix:    prefix,
		startTime: time.Now(),
	}
}

// Write implements io.Writer for Console Writer with
func (cw *consoleLogWriter) Write(b []byte) (int, error) {
	since := time.Since(cw.startTime).Truncate(time.Millisecond)
	prefix := []byte(fmt.Sprintf("%s [+%v]: ", cw.prefix, since))
	suffix := []byte("\n")

	_, _ = os.Stdout.Write(prefix)
	n, err := os.Stdout.Write(b)
	_, _ = os.Stdout.Write(suffix)
	return n, err
}

// getLogStream returns the logs stream for a specific container in pod.
func (cp *cosaPod) getLogStream(container string) (io.ReadCloser, error) {
	cs, ns, err := GetClient(cp.clusterCtx)
	if err != nil {
		return nil, err
	}
	podLogOpts := v1.PodLogOptions{
		Follow:    true,
		Container: container,
	}

	req := cs.CoreV1().Pods(ns).GetLogs(cp.pod.Name, &podLogOpts)
	return req.Stream()
}

// writeToWriters writes in to outs until in or outs are closed. When run a
// go-routine, calls can terminate by closing "in".
func writeToWriters(l *log.Entry, in io.ReadCloser, outs ...io.Writer) <-chan error {
	outCh := make(chan error)
	go func() {
		var err error
		defer func() {
			if err != nil {
				l.WithError(err).Error("writeToWriters encountered an error")
				outCh <- err
			}
			l.Debug("closed writer to logs")
		}()

		scanner := bufio.NewScanner(in)
		outWriter := io.MultiWriter(outs...)
		for scanner.Scan() {
			_, err = outWriter.Write(scanner.Bytes())
			if err != nil {
				return
			}
		}
		err = scanner.Err()
		if err != nil {
			return
		}
	}()
	return outCh
}

// streamPodLogs steams the pod's logs to logging and to disk. Worker
// pods are responsible for their work, but not for their logs.
// To make streamPodLogs thread safe and non-blocking, it expects
// a pointer to a bool. If that pointer is nil or true, then we return.
func (cp *cosaPod) streamPodLogs() error {
	if cp.kubeResultCh == nil {
		return errors.New("result channel has not been opened")
	}

	for _, pC := range append(cp.pod.Spec.InitContainers, cp.pod.Spec.Containers...) {
		container := pC.Name

		streamer, err := cp.getLogStream(container)
		if err != nil {
			return nil
		}

		// Create the deafault file log
		logD := filepath.Join(cosaSrvDir, "logs")
		logN := filepath.Join(logD, fmt.Sprintf("%s-%s.log", cp.pod.Name, container))
		if err := os.MkdirAll(logD, 0755); err != nil {
			return fmt.Errorf("failed to create logs directory: %w", err)
		}
		logf, err := os.OpenFile(logN, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to create log for pod %s/%s container: %w", cp.pod.Name, container, err)
		}

		l := log.WithFields(log.Fields{
			"logfile":   logf.Name,
			"container": container,
			"pod":       cp.pod.Name,
		})

		// Watch the logs until the termination is singaled OR the logs stream fails.
		go func() {

			// the defer will ensure that writeToWriters errors and terminates
			defer func() {
				lerr := logf.Close()
				serr := streamer.Close()
				if lerr != nil || serr != nil {
					l.WithFields(log.Fields{
						"stream err": err,
						"log err":    lerr,
					}).Info("failed closing logs, likely will have dangling go-routines")
				}
				l.Info("logging terminated")
			}()

			for {
				select {
				case die, ok := <-cp.terminate:
					if die || !ok {
						return
					}
				case events, ok := <-cp.kubeResultCh():
					if !ok {
						return
					}
					resp := events.Object.(*v1.Pod)
					status := resp.Status
					if status.Phase != v1.PodRunning {
						return
					}
					l.WithField("status", status.Phase).Debug("logging is waiting")
				case err := <-writeToWriters(l, streamer, logf, newConsoleLogWriter(container)):
					if err != nil {
						l.WithError(err).Warn("error recieved from writer")
						return
					}
				}
			}
		}()
	}
	return nil
}

// outWriteCloser is a noop closer
type outWriteCloser struct {
	*os.File
}

func (o *outWriteCloser) Close() error {
	return nil
}

func newNoopFileWriterCloser(f *os.File) *outWriteCloser {
	return &outWriteCloser{f}
}

// podmanRunner runs the work in a Podman container using workDir as `/srv`
// `podman kube play` does not work well due to permission mappings; there is
// no way to do id mappings.
func podmanRunner(ctx ClusterContext, term <-chan bool, cp *cosaPod, envVars []v1.EnvVar) error {
	// Populate pod envvars
	envVars = append(envVars, v1.EnvVar{Name: localPodEnvVar, Value: "1"})
	mapEnvVars := map[string]string{
		localPodEnvVar: "1",
	}
	for _, v := range envVars {
		mapEnvVars[v.Name] = v.Value
	}

	// Get our pod spec
	podSpec := cp.getPodSpec(nil)
	l := log.WithFields(log.Fields{
		"method":  "podman",
		"image":   podSpec.Spec.Containers[0].Image,
		"podName": podSpec.Name,
	})

	cmd := exec.Command("systemctl", "--user", "start", "podman.socket")
	if err := cmd.Run(); err != nil {
		l.WithError(err).Fatal("Failed to start podman socket")
	}
	sockDir := os.Getenv("XDG_RUNTIME_DIR")
	socket := "unix:" + sockDir + "/podman/podman.sock"

	// Connect to Podman socket
	connText, err := bindings.NewConnection(ctx, socket)
	if err != nil {
		return err
	}

	rt, err := libpod.NewRuntime(connText)
	if err != nil {
		return fmt.Errorf("failed to get container runtime: %w", err)
	}

	// Get the StdIO from the cluster context.
	clusterCtx, err := GetCluster(ctx)
	if err != nil {
		return err
	}
	stdIn, stdOut, stdErr := clusterCtx.GetStdIO()
	if stdOut == nil {
		stdOut = os.Stdout
	}
	if stdErr == nil {
		stdErr = os.Stdout
	}
	if stdIn == nil {
		stdIn = os.Stdin
	}

	streams := &define.AttachStreams{
		AttachError:  true,
		AttachOutput: true,
		AttachInput:  true,
		InputStream:  bufio.NewReader(stdIn),
		OutputStream: newNoopFileWriterCloser(stdOut),
		ErrorStream:  newNoopFileWriterCloser(stdErr),
	}

	s := specgen.NewSpecGenerator(podSpec.Spec.Containers[0].Image)
	s.CapAdd = podmanCaps
	s.Name = podSpec.Name
	s.Entrypoint = []string{"/usr/bin/dumb-init", "/usr/bin/gangplank", "builder"}
	s.ContainerNetworkConfig = specgen.ContainerNetworkConfig{
		NetNS: specgen.Namespace{
			NSMode: specgen.Host,
		},
	}

	u, err := user.CurrentUser()
	if err != nil {
		return fmt.Errorf("unable to lookup the current user: %v", err)
	}

	s.ContainerSecurityConfig = specgen.ContainerSecurityConfig{
		Privileged: true,
		User:       "builder",
		IDMappings: &storage.IDMappingOptions{
			UIDMap: []idtools.IDMap{
				{
					ContainerID: 0,
					HostID:      u.Uid,
					Size:        1,
				},
				{
					ContainerID: 1000,
					HostID:      u.Uid,
					Size:        200000,
				},
			},
		},
	}
	s.Env = mapEnvVars
	s.Stdin = true
	s.Terminal = true
	s.Devices = []cspec.LinuxDevice{
		{
			Path: "/dev/kvm",
			Type: "char",
		},
		{
			Path: "/dev/fuse",
			Type: "char",
		},
	}

	// Ensure that /srv in the COSA container is defined.
	srvDir := clusterCtx.podmanSrvDir
	if srvDir == "" {
		// ioutil.TempDir does not create the directory with the appropriate perms
		tmpSrvDir := filepath.Join(cosaSrvDir, s.Name)
		if err := os.MkdirAll(tmpSrvDir, 0777); err != nil {
			return fmt.Errorf("failed to create emphemeral srv dir for pod: %w", err)
		}
		srvDir = tmpSrvDir

		// ensure that the correct selinux context is set, otherwise wierd errors
		// in CoreOS Assembler will be emitted.
		args := []string{"chcon", "-R", "system_u:object_r:container_file_t:s0", srvDir}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if err := cmd.Run(); err != nil {
			l.WithError(err).Fatalf("failed set selinux context on %s", srvDir)
		}
	}

	l.WithField("bind mount", srvDir).Info("using host directory for /srv")
	s.WorkDir = "/srv"
	s.Mounts = []cspec.Mount{
		{
			Type:        "bind",
			Destination: "/srv",
			Source:      srvDir,
		},
	}
	s.Entrypoint = []string{"/usr/bin/dumb-init"}
	s.Command = []string{"/usr/bin/gangplank", "builder"}

	// Validate and define the container spec
	if err := s.Validate(); err != nil {
		l.WithError(err).Error("Validation failed")
	}
	r, err := containers.CreateWithSpec(connText, s)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	// Look up the container.
	lb, err := rt.LookupContainer(r.ID)
	if err != nil {
		return fmt.Errorf("failed to find container: %w", err)
	}

	// Manually terminate the pod to ensure that we get all the logs first.
	// Here be hacks: the API is dreadful for streaming logs. Podman,
	// in this case, is a better UX. There likely is a much better way, but meh,
	// this works.
	ender := func() {
		time.Sleep(1 * time.Second)
		_ = containers.Remove(connText, r.ID, ptrBool(true), ptrBool(true))
		if clusterCtx.podmanSrvDir != "" {
			return
		}

		l.Info("Cleaning up ephemeral /srv")
		defer os.RemoveAll(srvDir) //nolint

		s.User = "root"
		s.Entrypoint = []string{"/bin/rm", "-rvf", "/srv/"}
		s.Name = fmt.Sprintf("%s-cleaner", s.Name)
		cR, _ := containers.CreateWithSpec(connText, s)
		defer containers.Remove(connText, cR.ID, ptrBool(true), ptrBool(true)) //nolint

		if err := containers.Start(connText, cR.ID, nil); err != nil {
			l.WithError(err).Info("Failed to start cleanup conatiner")
			return
		}
		_, err := containers.Wait(connText, cR.ID, nil)
		if err != nil {
			l.WithError(err).Error("Failed")
		}
	}
	defer ender()

	if err := containers.Start(connText, r.ID, nil); err != nil {
		l.WithError(err).Error("Start of pod failed")
		return err
	}

	go func() {
		select {
		case <-ctx.Done():
			ender()
		case <-term:
			ender()
		}
	}()

	l.WithFields(log.Fields{
		"stdIn":  stdIn.Name(),
		"stdOut": stdOut.Name(),
		"stdErr": stdErr.Name(),
	}).Info("binding stdio to continater")
	resize := make(chan remotecommand.TerminalSize)

	go func() {
		_ = lb.Attach(streams, "", resize)
	}()

	if rc, _ := lb.Wait(); rc != 0 {
		return errors.New("work pod failed")
	}
	return nil
}
