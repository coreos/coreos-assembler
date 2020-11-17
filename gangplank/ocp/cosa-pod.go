package ocp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containers/libpod/libpod/define"
	"github.com/containers/libpod/pkg/bindings"
	"github.com/containers/libpod/pkg/bindings/containers"
	"github.com/containers/libpod/pkg/specgen"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/idtools"
	cspec "github.com/opencontainers/runtime-spec/specs-go"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	kubeJSON "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
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
		"/bin/bash",
		"-c",
		"'chmod 0666 /dev/kvm'",
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
	apiBuild     *buildapiv1.Build
	apiClientSet *kubernetes.Clientset
	project      string

	ocpInitCommand  []string
	ocpRequirements v1.ResourceList
	ocpSecContext   *v1.SecurityContext
	volumes         []v1.Volume
	volumeMounts    []v1.VolumeMount

	index int
	pod   *v1.Pod
}

// CosaPodder create COSA capable pods.
type CosaPodder interface {
	WorkerRunner(ctx context.Context, envVar []v1.EnvVar) error
}

// a cosaPod is a CosaPodder
var _ = CosaPodder(&cosaPod{})

// NewCosaPodder creates a CosaPodder
func NewCosaPodder(
	ctx context.Context,
	apiBuild *buildapiv1.Build,
	apiClientSet *kubernetes.Clientset,
	project string,
	index int) (CosaPodder, error) {

	cp := &cosaPod{
		apiBuild:     apiBuild,
		apiClientSet: apiClientSet,
		project:      project,
		index:        index,

		// Set defaults for OpenShift 4.x
		ocpRequirements: ocpRequirements,
		ocpSecContext:   ocpSecContext,
		ocpInitCommand:  ocpInitCommand,

		volumes:      volumes,
		volumeMounts: volumeMounts,
	}

	if cp.apiClientSet != nil {
		vi, err := cp.apiClientSet.DiscoveryClient.ServerVersion()
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
	}

	return cp, nil
}

func ptrInt(i int64) *int64 { return &i }
func ptrBool(b bool) *bool  { return &b }

// getPodSpec returns a pod specification
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
			Limits:   ocpRequirements,
			Requests: ocpRequirements,
		},
	}

	cosaWork := []v1.Container{cosaBasePod}

	cosaInit := []v1.Container{}
	if len(ocpInitCommand) > 0 {
		log.Infof("InitContainer has been defined")
		cosaInit := cosaBasePod.DeepCopy()
		cosaInit.Command = ocpInitCommand[:0]
		cosaInit.Args = ocpInitCommand[1:]
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
			Volumes:                       volumes,
		},
	}
}

// WorkerRunner runs a worker pod and watches until finished
func (cp *cosaPod) WorkerRunner(ctx context.Context, envVars []v1.EnvVar) error {
	if cp.apiClientSet != nil {
		return clusterRunner(ctx, cp, envVars)
	}
	return podmanRunner(ctx, cp, envVars)
}

func clusterRunner(ctx context.Context, cp *cosaPod, envVars []v1.EnvVar) error {
	pod := cp.getPodSpec(envVars)
	ac := cp.apiClientSet.CoreV1()
	resp, err := ac.Pods(cp.project).Create(pod)
	if err != nil {
		return fmt.Errorf("failed to create pod %s: %w", pod.Name, err)
	}
	log.Infof("Pod created: %s", pod.Name)
	cp.pod = pod

	status := resp.Status
	w, err := ac.Pods(cp.project).Watch(
		metav1.ListOptions{
			Watch:           true,
			ResourceVersion: resp.ResourceVersion,
			FieldSelector:   fields.Set{"metadata.name": pod.Name}.AsSelector().String(),
			LabelSelector:   labels.Everything().String(),
		},
	)
	if err != nil {
		return err
	}
	defer w.Stop()

	l := log.WithField("podname", pod.Name)

	// ender is our clean-up that kill our pods
	ender := func() {
		l.Infof("terminating")
		if err := ac.Pods(cp.project).Delete(pod.Name, &metav1.DeleteOptions{}); err != nil {
			l.WithError(err).Error("Failed delete on pod, yolo.")
		}
	}
	defer ender()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)

	logStarted := false
	// Block waiting for the pod to finish or timeout.
	for {
		select {
		case events, ok := <-w.ResultChan():
			if !ok {
				l.Error("failed waitching pod")
				return fmt.Errorf("orphaned pod")
			}
			resp = events.Object.(*v1.Pod)
			status = resp.Status

			l := log.WithFields(log.Fields{
				"podname": pod.Name,
				"status":  resp.Status.Phase,
			})
			switch sp := status.Phase; sp {
			case v1.PodSucceeded:
				l.Infof("Pod successfully completed")
				return nil
			case v1.PodRunning:
				l.Infof("Pod successfully completed")
				if err := cp.streamPodLogs(&logStarted, pod); err != nil {
					l.WithField("err", err).Error("failed to open logging")
				}
			case v1.PodFailed:
				l.WithField("message", status.Message).Error("Pod failed")
				return fmt.Errorf("Pod is a failure in its life")
			default:
				l.WithField("message", status.Message).Info("waiting...")
			}

		// Ensure a dreadful and uncerimonious end to our job in case of
		// a timeout, the buildconfig is terminated, or there's a cancellation.
		case <-time.After(90 * time.Minute):
			return errors.New("Pod did not complete work in time")
		case <-sigs:
			ender()
			return errors.New("Termination requested")
		case <-ctx.Done():
			return nil
		}
	}
}

// streamPodLogs steams the pod's logs to logging and to disk. Worker
// pods are responsible for their work, but not for their logs.
// To make streamPodLogs thread safe and non-blocking, it expects
// a pointer to a bool. If that pointer is nil or true, then we return
func (cp *cosaPod) streamPodLogs(logging *bool, pod *v1.Pod) error {
	if logging != nil && *logging {
		return nil
	}
	*logging = true
	podLogOpts := v1.PodLogOptions{
		Follow: true,
	}
	req := cp.apiClientSet.CoreV1().Pods(cp.project).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream()
	if err != nil {
		return err
	}

	lF := log.Fields{"pod": pod.Name}

	logD := filepath.Join(cosaSrvDir, "logs")
	podLog := filepath.Join(logD, fmt.Sprintf("%s.log", pod.Name))
	if err := os.MkdirAll(logD, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	logf, err := os.OpenFile(podLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log for pod %s: %w", pod.Name, err)
	}

	// Make the logging non-blocking to allow for concurrent pods
	// to be doing their thing(s).
	// TODO: decide on whether to use logrus (structured logging), or leave
	//       on screen (logrus was some ugly text). Logs are saved to
	//       /srv/logs/<pod.Name>.log which should be good enough.
	go func(logging *bool, logf *os.File) {
		defer func() { logging = ptrBool(false) }()
		defer podLogs.Close()

		startTime := time.Now()

		for {
			scanner := bufio.NewScanner(podLogs)
			for scanner.Scan() {
				since := time.Since(startTime).Truncate(time.Millisecond)
				fmt.Printf("%s [+%v]: %s\n", pod.Name, since, scanner.Text())
				if _, err := logf.Write(scanner.Bytes()); err != nil {
					log.WithFields(log.Fields{
						"pod":   pod.Name,
						"error": fmt.Sprintf("%v", err),
					}).Warnf("unable to log to file")
				}
			}
			if err := scanner.Err(); err != nil {
				if err == io.EOF {
					log.WithFields(lF).Info("Log closed")
					return
				}
				log.WithFields(log.Fields{
					"pod":   pod.Name,
					"error": fmt.Sprintf("%v", err),
				}).Warn("error scanning output")
			}
		}
	}(logging, logf)

	return nil
}

// encodeToJSON renders the JSON definition of a COSA pod. Podman prefers
// non-pretty JSON.
//nolint
func encodeToJSON(pod *v1.Pod, envVars []v1.EnvVar) (string, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return "", err
	}
	s := runtime.NewScheme()
	e := kubeJSON.NewSerializerWithOptions(
		kubeJSON.DefaultMetaFactory, s, s,
		//kubeJSON.SerializerOptions{Pretty: true}
		json.SerializerOptions{Yaml: true, Pretty: false},
	)

	var d bytes.Buffer
	dW := io.Writer(&d)

	if err := e.Encode(pod, dW); err != nil {
		return "", err
	}

	return d.String(), nil
}

// podmanRunner runs the work in a Podman container using workDir as `/srv`
// `podman kube play` does not work well due to permission mappings; there is
// no way to do id mappings.
func podmanRunner(ctx context.Context, cp *cosaPod, envVars []v1.EnvVar) error {
	envVars = append(envVars, v1.EnvVar{Name: localPodEnvVar, Value: "1"})

	// Populate pod envvars
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

	s := specgen.NewSpecGenerator(podSpec.Spec.Containers[0].Image)
	s.CapAdd = podmanCaps
	s.Name = podSpec.Name
	s.ContainerNetworkConfig = specgen.ContainerNetworkConfig{
		NetNS: specgen.Namespace{
			NSMode: specgen.Host,
		},
	}
	s.ContainerSecurityConfig = specgen.ContainerSecurityConfig{
		Privileged: true,
		User:       "builder",
		IDMappings: &storage.IDMappingOptions{
			UIDMap: []idtools.IDMap{
				{
					ContainerID: 0,
					HostID:      1000,
					Size:        1,
				},
				{
					ContainerID: 1000,
					HostID:      1000,
					Size:        200000,
				},
			},
		},
	}
	s.Env = mapEnvVars
	s.WorkDir = "/srv"
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
	s.Mounts = []cspec.Mount{
		{
			Type:        "bind",
			Destination: "/srv",
			Source:      cosaSrvDir,
		},
	}

	if err := s.Validate(); err != nil {
		l.WithError(err).Error("Validation failed")
	}

	r, err := containers.CreateWithSpec(connText, s)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Manually terminate the pod to ensure that we get all the logs first.
	// Here be hacks: the API is dreadful for streaming logs. Podman,
	// in this case, is a better UX. There likely is a much better way, but meh,
	// this works.
	logCmd := exec.CommandContext(ctx, "podman", "logs", "--follow", podSpec.Name)
	logCmd.Stderr = os.Stdout
	logCmd.Stdout = os.Stdout
	ender := func() {
		time.Sleep(1 * time.Second)
		_ = containers.Remove(connText, r.ID, ptrBool(true), ptrBool(true))
		_ = logCmd.Process.Kill()
	}
	defer ender()

	// Ensure clean-up on signal, i.e. ctrl-c
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		<-sigs
		ender()
	}()

	l.Info("Starting pod")
	if err := containers.Start(connText, r.ID, nil); err != nil {
		l.WithError(err).Error("Start of pod failed")
		return err
	}

	l.Info("Checking on pod")
	running := define.ContainerStateRunning
	if _, err = containers.Wait(connText, r.ID, &running); err != nil {
		l.WithError(err).Error("Check failed")
	}

	// Start logging
	go func() {
		_ = logCmd.Run()
	}()

	rc, err := containers.Wait(connText, r.ID, nil)
	if err != nil {
		l.WithError(err).Error("Failed")
	}
	if rc != 0 {
		l.WithField("rc", rc).Error("Failed to execute pod")
		return errors.New("pod workload failed")
	}

	return nil
}
