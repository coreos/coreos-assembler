package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/containers/libpod/libpod"
	"github.com/containers/libpod/libpod/define"
	"github.com/containers/libpod/libpod/events"
	libpodImage "github.com/containers/libpod/libpod/image"
	"github.com/containers/libpod/pkg/domain/entities"
	docker "github.com/docker/docker/api/types"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerEvents "github.com/docker/docker/api/types/events"
	dockerNetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
)

type AuthConfig struct {
	docker.AuthConfig
}

type ImageInspect struct {
	docker.ImageInspect
}

type ContainerConfig struct {
	dockerContainer.Config
}

type LibpodImagesLoadReport struct {
	ID string `json:"id"`
}

type LibpodImagesPullReport struct {
	ID string `json:"id"`
}

type ContainersPruneReport struct {
	docker.ContainersPruneReport
}

type LibpodContainersPruneReport struct {
	ID             string `json:"id"`
	SpaceReclaimed int64  `json:"space"`
	PruneError     string `json:"error"`
}

type Info struct {
	docker.Info
	BuildahVersion     string
	CPURealtimePeriod  bool
	CPURealtimeRuntime bool
	CgroupVersion      string
	Rootless           bool
	SwapFree           int64
	SwapTotal          int64
	Uptime             string
}

type Container struct {
	docker.Container
	docker.ContainerCreateConfig
}

type Version struct {
	docker.Version
}

type DiskUsage struct {
	docker.DiskUsage
}

type VolumesPruneReport struct {
	docker.VolumesPruneReport
}

type ImagesPruneReport struct {
	docker.ImagesPruneReport
}

type BuildCachePruneReport struct {
	docker.BuildCachePruneReport
}

type NetworkPruneReport struct {
	docker.NetworksPruneReport
}

type ConfigCreateResponse struct {
	docker.ConfigCreateResponse
}

type PushResult struct {
	docker.PushResult
}

type BuildResult struct {
	docker.BuildResult
}

type ContainerWaitOKBody struct {
	StatusCode int
	Error      struct {
		Message string
	}
}

type CreateContainerConfig struct {
	Name string
	dockerContainer.Config
	HostConfig       dockerContainer.HostConfig
	NetworkingConfig dockerNetwork.NetworkingConfig
}

// swagger:model IDResponse
type IDResponse struct {
	// ID
	ID string `json:"id"`
}

type ContainerTopOKBody struct {
	dockerContainer.ContainerTopOKBody
}

type PodTopOKBody struct {
	dockerContainer.ContainerTopOKBody
}

// swagger:model PodCreateConfig
type PodCreateConfig struct {
	Name         string   `json:"name"`
	CGroupParent string   `json:"cgroup-parent"`
	Hostname     string   `json:"hostname"`
	Infra        bool     `json:"infra"`
	InfraCommand string   `json:"infra-command"`
	InfraImage   string   `json:"infra-image"`
	Labels       []string `json:"labels"`
	Publish      []string `json:"publish"`
	Share        string   `json:"share"`
}

type ErrorModel struct {
	Message string `json:"message"`
}

type Event struct {
	dockerEvents.Message
}

type HistoryResponse struct {
	ID        string   `json:"Id"`
	Created   int64    `json:"Created"`
	CreatedBy string   `json:"CreatedBy"`
	Tags      []string `json:"Tags"`
	Size      int64    `json:"Size"`
	Comment   string   `json:"Comment"`
}

type ImageLayer struct{}

type ImageTreeResponse struct {
	ID     string       `json:"id"`
	Tags   []string     `json:"tags"`
	Size   string       `json:"size"`
	Layers []ImageLayer `json:"layers"`
}

type ExecCreateConfig struct {
	docker.ExecConfig
}

type ExecCreateResponse struct {
	docker.IDResponse
}

func EventToApiEvent(e *events.Event) *Event {
	return &Event{dockerEvents.Message{
		Type:   e.Type.String(),
		Action: e.Status.String(),
		Actor: dockerEvents.Actor{
			ID: e.ID,
			Attributes: map[string]string{
				"image":             e.Image,
				"name":              e.Name,
				"containerExitCode": strconv.Itoa(e.ContainerExitCode),
			},
		},
		Scope:    "local",
		Time:     e.Time.Unix(),
		TimeNano: e.Time.UnixNano(),
	}}
}

func ImageToImageSummary(l *libpodImage.Image) (*entities.ImageSummary, error) {
	containers, err := l.Containers()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to obtain Containers for image %s", l.ID())
	}
	containerCount := len(containers)

	// FIXME: GetParent() panics
	// parent, err := l.GetParent(context.TODO())
	// if err != nil {
	// 	return nil, errors.Wrapf(err, "Failed to obtain ParentID for image %s", l.ID())
	// }

	labels, err := l.Labels(context.TODO())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to obtain Labels for image %s", l.ID())
	}

	size, err := l.Size(context.TODO())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to obtain Size for image %s", l.ID())
	}

	repoTags, err := l.RepoTags()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to obtain RepoTags for image %s", l.ID())
	}

	history, err := l.History(context.TODO())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to obtain History for image %s", l.ID())
	}
	historyIds := make([]string, len(history))
	for i, h := range history {
		historyIds[i] = h.ID
	}

	digests := make([]string, len(l.Digests()))
	for i, d := range l.Digests() {
		digests[i] = string(d)
	}

	is := entities.ImageSummary{
		ID:           l.ID(),
		ParentId:     l.Parent,
		RepoTags:     repoTags,
		Created:      l.Created().Unix(),
		Size:         int64(*size),
		SharedSize:   0,
		VirtualSize:  l.VirtualSize,
		Labels:       labels,
		Containers:   containerCount,
		ReadOnly:     l.IsReadOnly(),
		Dangling:     l.Dangling(),
		Names:        l.Names(),
		Digest:       string(l.Digest()),
		Digests:      digests,
		ConfigDigest: string(l.ConfigDigest),
		History:      historyIds,
	}
	return &is, nil
}

func ImageDataToImageInspect(ctx context.Context, l *libpodImage.Image) (*ImageInspect, error) {
	info, err := l.Inspect(context.Background())
	if err != nil {
		return nil, err
	}
	ports, err := portsToPortSet(info.Config.ExposedPorts)
	if err != nil {
		return nil, err
	}
	// TODO the rest of these still need wiring!
	config := dockerContainer.Config{
		//	Hostname:        "",
		//	Domainname:      "",
		User: info.User,
		//	AttachStdin:     false,
		//	AttachStdout:    false,
		//	AttachStderr:    false,
		ExposedPorts: ports,
		//	Tty:             false,
		//	OpenStdin:       false,
		//	StdinOnce:       false,
		Env: info.Config.Env,
		Cmd: info.Config.Cmd,
		//	Healthcheck:     nil,
		//	ArgsEscaped:     false,
		//	Image:           "",
		//	Volumes:         nil,
		//	WorkingDir:      "",
		//	Entrypoint:      nil,
		//	NetworkDisabled: false,
		//	MacAddress:      "",
		//	OnBuild:         nil,
		//	Labels:          nil,
		//	StopSignal:      "",
		//	StopTimeout:     nil,
		//	Shell:           nil,
	}
	ic, err := l.ToImageRef(ctx)
	if err != nil {
		return nil, err
	}
	dockerImageInspect := docker.ImageInspect{
		Architecture:  l.Architecture,
		Author:        l.Author,
		Comment:       info.Comment,
		Config:        &config,
		Created:       l.Created().Format(time.RFC3339Nano),
		DockerVersion: "",
		GraphDriver:   docker.GraphDriverData{},
		ID:            fmt.Sprintf("sha256:%s", l.ID()),
		Metadata:      docker.ImageMetadata{},
		Os:            l.Os,
		OsVersion:     l.Version,
		Parent:        l.Parent,
		RepoDigests:   info.RepoDigests,
		RepoTags:      info.RepoTags,
		RootFS:        docker.RootFS{},
		Size:          info.Size,
		Variant:       "",
		VirtualSize:   info.VirtualSize,
	}
	bi := ic.ConfigInfo()
	// For docker images, we need to get the Container id and config
	// and populate the image with it.
	if bi.MediaType == manifest.DockerV2Schema2ConfigMediaType {
		d := manifest.Schema2Image{}
		b, err := ic.ConfigBlob(ctx)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		// populate the Container id into the image
		dockerImageInspect.Container = d.Container
		containerConfig := dockerContainer.Config{}
		configBytes, err := json.Marshal(d.ContainerConfig)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(configBytes, &containerConfig); err != nil {
			return nil, err
		}
		// populate the Container config in the image
		dockerImageInspect.ContainerConfig = &containerConfig
		// populate parent
		dockerImageInspect.Parent = d.Parent.String()
	}
	return &ImageInspect{dockerImageInspect}, nil

}

func LibpodToContainer(l *libpod.Container, sz bool) (*Container, error) {
	imageId, imageName := l.Image()

	var (
		err        error
		sizeRootFs int64
		sizeRW     int64
		state      define.ContainerStatus
	)

	if state, err = l.State(); err != nil {
		return nil, err
	}
	stateStr := state.String()
	if stateStr == "configured" {
		stateStr = "created"
	}

	if sz {
		if sizeRW, err = l.RWSize(); err != nil {
			return nil, err
		}
		if sizeRootFs, err = l.RootFsSize(); err != nil {
			return nil, err
		}
	}

	return &Container{docker.Container{
		ID:         l.ID(),
		Names:      []string{fmt.Sprintf("/%s", l.Name())},
		Image:      imageName,
		ImageID:    imageId,
		Command:    strings.Join(l.Command(), " "),
		Created:    l.CreatedTime().Unix(),
		Ports:      nil,
		SizeRw:     sizeRW,
		SizeRootFs: sizeRootFs,
		Labels:     l.Labels(),
		State:      stateStr,
		Status:     "",
		HostConfig: struct {
			NetworkMode string `json:",omitempty"`
		}{
			"host"},
		NetworkSettings: nil,
		Mounts:          nil,
	},
		docker.ContainerCreateConfig{},
	}, nil
}

func LibpodToContainerJSON(l *libpod.Container, sz bool) (*docker.ContainerJSON, error) {
	_, imageName := l.Image()
	inspect, err := l.Inspect(sz)
	if err != nil {
		return nil, err
	}
	i, err := json.Marshal(inspect.State)
	if err != nil {
		return nil, err
	}
	state := docker.ContainerState{}
	if err := json.Unmarshal(i, &state); err != nil {
		return nil, err
	}

	// docker considers paused to be running
	if state.Paused {
		state.Running = true
	}

	h, err := json.Marshal(inspect.HostConfig)
	if err != nil {
		return nil, err
	}
	hc := dockerContainer.HostConfig{}
	if err := json.Unmarshal(h, &hc); err != nil {
		return nil, err
	}
	g, err := json.Marshal(inspect.GraphDriver)
	if err != nil {
		return nil, err
	}
	graphDriver := docker.GraphDriverData{}
	if err := json.Unmarshal(g, &graphDriver); err != nil {
		return nil, err
	}

	cb := docker.ContainerJSONBase{
		ID:              l.ID(),
		Created:         l.CreatedTime().String(),
		Path:            "",
		Args:            nil,
		State:           &state,
		Image:           imageName,
		ResolvConfPath:  inspect.ResolvConfPath,
		HostnamePath:    inspect.HostnamePath,
		HostsPath:       inspect.HostsPath,
		LogPath:         l.LogPath(),
		Node:            nil,
		Name:            fmt.Sprintf("/%s", l.Name()),
		RestartCount:    0,
		Driver:          inspect.Driver,
		Platform:        "linux",
		MountLabel:      inspect.MountLabel,
		ProcessLabel:    inspect.ProcessLabel,
		AppArmorProfile: inspect.AppArmorProfile,
		ExecIDs:         inspect.ExecIDs,
		HostConfig:      &hc,
		GraphDriver:     graphDriver,
		SizeRw:          inspect.SizeRw,
		SizeRootFs:      &inspect.SizeRootFs,
	}

	stopTimeout := int(l.StopTimeout())

	ports := make(nat.PortSet)
	for p := range inspect.HostConfig.PortBindings {
		splitp := strings.Split(p, "/")
		port, err := nat.NewPort(splitp[0], splitp[1])
		if err != nil {
			return nil, err
		}
		ports[port] = struct{}{}
	}

	config := dockerContainer.Config{
		Hostname:        l.Hostname(),
		Domainname:      inspect.Config.DomainName,
		User:            l.User(),
		AttachStdin:     inspect.Config.AttachStdin,
		AttachStdout:    inspect.Config.AttachStdout,
		AttachStderr:    inspect.Config.AttachStderr,
		ExposedPorts:    ports,
		Tty:             inspect.Config.Tty,
		OpenStdin:       inspect.Config.OpenStdin,
		StdinOnce:       inspect.Config.StdinOnce,
		Env:             inspect.Config.Env,
		Cmd:             inspect.Config.Cmd,
		Healthcheck:     nil,
		ArgsEscaped:     false,
		Image:           imageName,
		Volumes:         nil,
		WorkingDir:      l.WorkingDir(),
		Entrypoint:      l.Entrypoint(),
		NetworkDisabled: false,
		MacAddress:      "",
		OnBuild:         nil,
		Labels:          l.Labels(),
		StopSignal:      string(l.StopSignal()),
		StopTimeout:     &stopTimeout,
		Shell:           nil,
	}

	m, err := json.Marshal(inspect.Mounts)
	if err != nil {
		return nil, err
	}
	mounts := []docker.MountPoint{}
	if err := json.Unmarshal(m, &mounts); err != nil {
		return nil, err
	}

	networkSettingsDefault := docker.DefaultNetworkSettings{
		EndpointID:          "",
		Gateway:             "",
		GlobalIPv6Address:   "",
		GlobalIPv6PrefixLen: 0,
		IPAddress:           "",
		IPPrefixLen:         0,
		IPv6Gateway:         "",
		MacAddress:          l.Config().StaticMAC.String(),
	}

	networkSettings := docker.NetworkSettings{
		NetworkSettingsBase:    docker.NetworkSettingsBase{},
		DefaultNetworkSettings: networkSettingsDefault,
		Networks:               nil,
	}

	c := docker.ContainerJSON{
		ContainerJSONBase: &cb,
		Mounts:            mounts,
		Config:            &config,
		NetworkSettings:   &networkSettings,
	}
	return &c, nil
}

// portsToPortSet converts libpods exposed ports to dockers structs
func portsToPortSet(input map[string]struct{}) (nat.PortSet, error) {
	ports := make(nat.PortSet)
	for k := range input {
		npTCP, err := nat.NewPort("tcp", k)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create tcp port from %s", k)
		}
		npUDP, err := nat.NewPort("udp", k)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create udp port from %s", k)
		}
		ports[npTCP] = struct{}{}
		ports[npUDP] = struct{}{}
	}
	return ports, nil
}
