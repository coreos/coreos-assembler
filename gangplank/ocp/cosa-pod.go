package ocp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	//	"k8s.io/client-go/rest/watch"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	kvmLabel = "devices.kubevirt.io/kvm"
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

	// On OCP3.x, we require privileges.
	ocp3SecContext = &v1.SecurityContext{
		RunAsUser:  ptrInt(0),
		RunAsGroup: ptrInt(1000),
		Privileged: ptrBool(true),
	}

	// InitCommands to be run before work pod is executed.
	ocpInitCommand = []string{}

	// on OCP v3, /dev/kvm is unlikely to world RW. So we have to give ourselves
	// permission. Gangplank will run as root but `cosa` commands run as the builder
	// user. Note: on OCP v4, gangplank will run unprivileged and OCP setups /dev/kvm
	// the way we need it.
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

// setPodDefaults checks the Kubernetes version to determine if
// we're on OCP 3.11 (v1.11) or 4.2(v1.15) or later.
// https://docs.openshift.com/container-platform/4.2/release_notes/ocp-4-2-release-notes.html#ocp-4-2-about-this-release
func setPodDefaults() error {
	vi, err := apiClientSet.DiscoveryClient.ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to query the kubernetes version: %w", err)
	}

	minor, err := strconv.Atoi(strings.TrimRight(vi.Minor, "+"))
	log.Infof("Kubernetes version of cluster is %s %s.%d", vi.String(), vi.Major, minor)
	if err != nil {
		return fmt.Errorf("failed to detect OCP cluster version: %v", err)
	}
	if minor >= 15 {
		log.Info("Detected OpenShift 4.x cluster")
		return nil
	}

	log.Infof("Creating container with Openshift v3.x defaults")
	ocpRequirements = ocp3Requirements
	ocpInitCommand = ocp3InitCommand
	ocpSecContext = ocp3SecContext
	return nil
}

func ptrInt(i int64) *int64 { return &i }

func ptrBool(b bool) *bool { return &b }

// Create creates a pod and ensures that the pod is cleaned up when the
// process finishes
// Inspired by https://github.com/kubernetes/client-go/blob/master/examples/create-update-delete-deployment/main.go
func createWorkerPod(ctx context.Context, index int, eVars []v1.EnvVar) error {
	if err := setPodDefaults(); err != nil {
		log.WithField("err", err).Error("assuming OpenShift version v4.x")
	}

	podName := fmt.Sprintf("%s-%s-worker-%d",
		apiBuild.Annotations[buildapiv1.BuildConfigAnnotation],
		apiBuild.Annotations[buildapiv1.BuildNumberAnnotation],
		index,
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
		Env:             eVars,
		WorkingDir:      "/srv",
		VolumeMounts:    volumeMounts,
		SecurityContext: ocpSecContext,
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

	req := &v1.Pod{
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

	resp, err := apiClient.Pods(projectNamespace).Create(req)
	if err != nil {
		return fmt.Errorf("failed to create pod %s: %w", podName, err)
	}

	log.Infof("Pod created: %s", podName)

	status := resp.Status
	w, err := apiClient.Pods(projectNamespace).Watch(
		metav1.ListOptions{
			Watch:           true,
			ResourceVersion: resp.ResourceVersion,
			FieldSelector:   fields.Set{"metadata.name": podName}.AsSelector().String(),
			LabelSelector:   labels.Everything().String(),
		},
	)
	if err != nil {
		return err
	}
	defer w.Stop()

	l := log.WithField("podname", podName)

	// ender is our clean-up that kill our pods
	ender := func() {
		l.Infof("terminating")
		if err := apiClient.Pods(projectNamespace).Delete(podName, &metav1.DeleteOptions{}); err != nil {
			l.WithField("err", err).Error("Failed delete on pod, yolo.")
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
				"podname": podName,
				"status":  resp.Status.Phase,
			})
			switch sp := status.Phase; sp {
			case v1.PodSucceeded:
				l.Infof("Pod successfully completed")
				return nil
			case v1.PodRunning:
				l.Infof("Pod successfully completed")
				if err := streamPodLogs(&logStarted, req); err != nil {
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
func streamPodLogs(logging *bool, pod *v1.Pod) error {
	if logging != nil && *logging {
		return nil
	}
	*logging = true
	podLogOpts := v1.PodLogOptions{
		Follow: true,
	}
	req := apiClient.Pods(projectNamespace).GetLogs(pod.Name, &podLogOpts)
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
				}).Warnf("error scanning output")
			}
		}
	}(logging, logf)

	return nil
}
