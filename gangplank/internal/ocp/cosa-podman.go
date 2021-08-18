// +build !gangway

package ocp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/containers/podman/v3/pkg/bindings"
	"github.com/containers/podman/v3/pkg/bindings/containers"
	podImages "github.com/containers/podman/v3/pkg/bindings/images"
	podVolumes "github.com/containers/podman/v3/pkg/bindings/volumes"
	"github.com/containers/podman/v3/pkg/domain/entities"
	"github.com/containers/podman/v3/pkg/specgen"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/idtools"
	"github.com/opencontainers/runc/libcontainer/user"
	cspec "github.com/opencontainers/runtime-spec/specs-go"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

// podmanContainerHostEnvVar is used by both Gangplank and the podman API
// to decide if the execution of the pod should happen over SSH.
const podmanContainerHostEnvVar = "CONTAINER_HOST"

func init() {
	podmanFunc = podmanRunner
}

// podmanRunner runs the work in a Podman container using workDir as `/srv`
// `podman kube play` does not work well due to permission mappings; there is
// no way to do id mappings.
func podmanRunner(term termChan, cp CosaPodder, envVars []v1.EnvVar) error {
	ctx := cp.GetClusterCtx()

	// Populate pod envvars
	envVars = append(
		envVars,
		v1.EnvVar{Name: localPodEnvVar, Value: "1"},
		v1.EnvVar{Name: "XDG_RUNTIME_DIR", Value: "/srv"},
	)
	mapEnvVars := map[string]string{}
	for _, v := range envVars {
		mapEnvVars[v.Name] = v.Value
	}

	// Get our pod spec
	podSpec, err := cp.getPodSpec(envVars)
	if err != nil {
		return err
	}
	l := log.WithFields(log.Fields{
		"method":  "podman",
		"image":   podSpec.Spec.Containers[0].Image,
		"podName": podSpec.Name,
	})

	// If a URI for the container API server has been specified
	// by the user then let's honor that. Else construct one.
	socket := os.Getenv(podmanContainerHostEnvVar)
	if strings.HasPrefix(socket, "ssh://") {
		l = l.WithField("podman socket", socket)
		l.Info("Lauching remote pod")
	} else {
		// Once podman 3.2.0 is released use this instead:
		//      import "github.com/containers/podman/v3/pkg/util"
		//      socket = util.SocketPath()
		sockDir := os.Getenv("XDG_RUNTIME_DIR")
		socket = "unix:" + sockDir + "/podman/podman.sock"
	}

	// Connect to Podman socket
	connText, err := bindings.NewConnection(ctx, socket)
	if err != nil {
		return err
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

	s := specgen.NewSpecGenerator(podSpec.Spec.Containers[0].Image, false)
	s.CapAdd = podmanCaps
	s.Name = podSpec.Name
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
		NoNewPrivileges: false,
		Umask:           "0022",
		Privileged:      true,
		User:            "builder",
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

	var srvVol *entities.VolumeConfigResponse = nil
	if clusterCtx.podmanSrvDir == "" {
		// If running podman remotely or the srvDir is undefined, create and use an ephemeral
		// volume. The volume will be removed via ender()
		srvVol, err = podVolumes.Create(connText, entities.VolumeCreateOptions{Name: podSpec.Name}, nil)
		if err != nil {
			return err
		}
		s.Volumes = []*specgen.NamedVolume{
			{
				Name:    srvVol.Name,
				Options: []string{},
				Dest:    "/srv",
			},
		}
		l.WithField("ephemeral vol", srvVol.Name).Info("using ephemeral volule for /srv")
	} else {
		// Otherwise, create a mount from the host container for /srv.
		l.WithField("bind mount", clusterCtx.podmanSrvDir).Info("using host directory for /srv")
		s.Mounts = []cspec.Mount{
			{
				Type:        "bind",
				Destination: "/srv",
				Source:      clusterCtx.podmanSrvDir,
			},
		}
	}

	s.WorkDir = "/srv"
	s.Entrypoint = []string{"/usr/bin/dumb-init"}
	s.Command = []string{gangwayCmd}

	if err := mustHaveImage(connText, s.Image); err != nil {
		return fmt.Errorf("failed checking/pulling image: %v", err)
	}

	// Validate and define the container spec
	if err := s.Validate(); err != nil {
		l.WithError(err).Error("Validation failed")
	}
	r, err := containers.CreateWithSpec(connText, s, nil)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// channels for logging
	consoleOutChan := make(chan string, 1024)

	// Manually terminate the pod to ensure that we get all the logs first.
	ender := func() {
		time.Sleep(1 * time.Second)
		_ = containers.Remove(connText, r.ID, new(containers.RemoveOptions).WithForce(true).WithVolumes(true))
		if srvVol != nil {
			_ = podVolumes.Remove(connText, srvVol.Name, nil)
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

	// Watch the logs.
	go func() {
		for {
			v, ok := <-consoleOutChan
			if !ok {
				return
			}
			os.Stdout.Write([]byte(v))
			os.Stdout.Write([]byte("\n"))
		}
	}()

	// Get the Logs. This will block until all logs are streamed.
	if err := containers.Logs(
		connText, r.ID,
		&containers.LogOptions{
			Follow: ptrBool(true),
		},
		consoleOutChan, consoleOutChan); err != nil {
		return fmt.Errorf("Failed setting up console monitoring: %v", err)
	}

	// Get the exit code of the container.
	rc, err := containers.Wait(connText, r.ID, nil)
	if rc != 0 {
		return fmt.Errorf("work pod failed: return code %d", rc)
	}
	return err
}

// mustHaveImage pulls the image if it is not found
func mustHaveImage(ctx context.Context, image string) error {
	found, err := podImages.Exists(ctx, image, nil)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = podImages.Pull(ctx, image, nil)
	return err
}
