// +build podman

package ocp

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/containers/podman/v3/pkg/bindings"
	"github.com/containers/podman/v3/pkg/bindings/containers"
	"github.com/containers/podman/v3/pkg/specgen"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/idtools"
	"github.com/opencontainers/runc/libcontainer/user"
	cspec "github.com/opencontainers/runtime-spec/specs-go"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

func init() {
	podmanFunc = podmanRunner
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
func podmanRunner(term termChan, cp CosaPodder, envVars []v1.EnvVar) error {
	ctx := cp.GetClusterCtx()
	// Populate pod envvars
	envVars = append(envVars, v1.EnvVar{Name: localPodEnvVar, Value: "1"})
	mapEnvVars := map[string]string{
		localPodEnvVar: "1",
	}
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
	socket := os.Getenv("CONTAINER_HOST")
	if socket == "" {
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
	s.Command = []string{gangwayCmd}

	// Validate and define the container spec
	if err := s.Validate(); err != nil {
		l.WithError(err).Error("Validation failed")
	}
	r, err := containers.CreateWithSpec(connText, s, nil)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Manually terminate the pod to ensure that we get all the logs first.
	// Here be hacks: the API is dreadful for streaming logs. Podman,
	// in this case, is a better UX. There likely is a much better way, but meh,
	// this works.
	ender := func() {
		time.Sleep(1 * time.Second)
		_ = containers.Remove(connText, r.ID, new(containers.RemoveOptions).WithForce(true).WithVolumes(true))
		if clusterCtx.podmanSrvDir != "" {
			return
		}

		l.Info("Cleaning up ephemeral /srv")
		defer os.RemoveAll(srvDir) //nolint

		s.User = "root"
		s.Entrypoint = []string{"/bin/rm", "-rvf", "/srv/"}
		s.Name = fmt.Sprintf("%s-cleaner", s.Name)
		cR, _ := containers.CreateWithSpec(connText, s, nil)
		defer containers.Remove(connText, cR.ID, new(containers.RemoveOptions).WithForce(true).WithVolumes(true)) //nolint

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

	go func() {
		_ = containers.Attach(connText, r.ID,
			bufio.NewReader(stdIn),
			newNoopFileWriterCloser(stdOut),
			newNoopFileWriterCloser(stdErr), nil, nil)
	}()

	if rc, _ := containers.Wait(connText, r.ID, nil); rc != 0 {
		return errors.New("work pod failed")
	}
	return nil
}
