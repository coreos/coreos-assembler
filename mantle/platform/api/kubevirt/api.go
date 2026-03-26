// Copyright 2025 Red Hat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubevirt

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/pkg/capnslog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kvv1 "kubevirt.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/kubevirt")
)

// Options holds KubeVirt platform configuration.
type Options struct {
	*platform.Options

	// Kubeconfig is the path to a kubeconfig file. If empty, uses
	// in-cluster config or the default ~/.kube/config.
	Kubeconfig string
	// Namespace is the Kubernetes namespace for VMs (default "default").
	Namespace string
	// Image is the containerDisk OCI image pull spec.
	Image string
	// CloudInitType is the default cloud-init type: "configdrive" or "nocloud".
	CloudInitType string
	// Memory is the VM memory (default "2Gi").
	Memory string
	// CPUs is the number of VM CPUs (default 2).
	CPUs uint32
}

// API wraps the Kubernetes/KubeVirt client for VM lifecycle management.
type API struct {
	opts   *Options
	client ctrlclient.Client
	config *rest.Config
}

// New creates a new KubeVirt API client.
func New(opts *Options) (*API, error) {
	if opts.Image == "" {
		return nil, fmt.Errorf("--kubevirt-image is required")
	}
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}
	if opts.CloudInitType == "" {
		opts.CloudInitType = "configdrive"
	}
	if opts.Memory == "" {
		opts.Memory = "2Gi"
	}
	if opts.CPUs == 0 {
		opts.CPUs = 2
	}

	// Build rest.Config
	var config *rest.Config
	var err error
	if opts.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", opts.Kubeconfig)
	} else {
		// Try in-cluster first, fall back to default kubeconfig
		config, err = rest.InClusterConfig()
		if err != nil {
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				loadingRules, configOverrides).ClientConfig()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %v", err)
	}

	// Register KubeVirt types
	scheme := runtime.NewScheme()
	if err := kvv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add kubevirt scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %v", err)
	}

	client, err := ctrlclient.New(config, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller-runtime client: %v", err)
	}

	return &API{
		opts:   opts,
		client: client,
		config: config,
	}, nil
}

// CreateVM creates a VirtualMachine with a containerDisk and cloud-init volume,
// then waits for the VMI to reach the Running phase.
func (a *API) CreateVM(name, userdata, cloudInitType, networkData string) error {
	running := true
	vm := a.buildVM(name, userdata, cloudInitType, networkData, running)

	ctx := context.Background()
	if err := a.client.Create(ctx, vm); err != nil {
		return fmt.Errorf("creating VirtualMachine %s: %v", name, err)
	}

	plog.Infof("Created VirtualMachine %s/%s, waiting for Running state", a.opts.Namespace, name)

	// Wait for VMI to reach Running phase
	if err := util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
		vmi := &kvv1.VirtualMachineInstance{}
		key := ctrlclient.ObjectKey{Namespace: a.opts.Namespace, Name: name}
		if err := a.client.Get(ctx, key, vmi); err != nil {
			return false, nil // VMI may not exist yet
		}
		switch vmi.Status.Phase {
		case kvv1.Running:
			plog.Infof("VMI %s is Running", name)
			return true, nil
		case kvv1.Failed:
			return false, fmt.Errorf("VMI %s entered Failed phase", name)
		default:
			return false, nil
		}
	}); err != nil {
		return fmt.Errorf("waiting for VMI %s: %v", name, err)
	}

	return nil
}

// DeleteVM deletes a VirtualMachine by name.
func (a *API) DeleteVM(name string) error {
	vm := &kvv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: a.opts.Namespace,
		},
	}
	ctx := context.Background()
	if err := a.client.Delete(ctx, vm); err != nil {
		return fmt.Errorf("deleting VirtualMachine %s: %v", name, err)
	}

	// Wait for VM to be gone
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		check := &kvv1.VirtualMachine{}
		key := ctrlclient.ObjectKey{Namespace: a.opts.Namespace, Name: name}
		err := a.client.Get(ctx, key, check)
		if err != nil {
			return true, nil // Gone
		}
		return false, nil
	})
}

// GetVMI retrieves the current VirtualMachineInstance status.
func (a *API) GetVMI(name string) (*kvv1.VirtualMachineInstance, error) {
	vmi := &kvv1.VirtualMachineInstance{}
	key := ctrlclient.ObjectKey{Namespace: a.opts.Namespace, Name: name}
	if err := a.client.Get(context.Background(), key, vmi); err != nil {
		return nil, fmt.Errorf("getting VMI %s: %v", name, err)
	}
	return vmi, nil
}

// Config returns the REST config for use by the port-forward tunnel.
func (a *API) Config() *rest.Config {
	return a.config
}

// Opts returns the API options.
func (a *API) Opts() *Options {
	return a.opts
}

// buildVM constructs the VirtualMachine CR.
func (a *API) buildVM(name, userdata, cloudInitType, networkData string, running bool) *kvv1.VirtualMachine {
	disks := []kvv1.Disk{
		{
			Name: "containerdisk",
			DiskDevice: kvv1.DiskDevice{
				Disk: &kvv1.DiskTarget{Bus: kvv1.DiskBusVirtio},
			},
		},
		{
			Name: "cloudinit",
			DiskDevice: kvv1.DiskDevice{
				Disk: &kvv1.DiskTarget{Bus: kvv1.DiskBusVirtio},
			},
		},
	}

	volumes := []kvv1.Volume{
		{
			Name: "containerdisk",
			VolumeSource: kvv1.VolumeSource{
				ContainerDisk: &kvv1.ContainerDiskSource{
					Image: a.opts.Image,
				},
			},
		},
	}

	// Build cloud-init volume based on type
	cloudInitVolume := kvv1.Volume{Name: "cloudinit"}
	switch cloudInitType {
	case "nocloud":
		source := &kvv1.CloudInitNoCloudSource{
			UserData: userdata,
		}
		if networkData != "" {
			source.NetworkData = networkData
		}
		cloudInitVolume.VolumeSource = kvv1.VolumeSource{
			CloudInitNoCloud: source,
		}
	default: // "configdrive"
		source := &kvv1.CloudInitConfigDriveSource{
			UserData: userdata,
		}
		if networkData != "" {
			source.NetworkData = networkData
		}
		cloudInitVolume.VolumeSource = kvv1.VolumeSource{
			CloudInitConfigDrive: source,
		}
	}
	volumes = append(volumes, cloudInitVolume)

	return &kvv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: a.opts.Namespace,
			Labels: map[string]string{
				"createdBy": "kola",
			},
		},
		Spec: kvv1.VirtualMachineSpec{
			Running: &running,
			Template: &kvv1.VirtualMachineInstanceTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"kubevirt.io/vm": name,
					},
				},
				Spec: kvv1.VirtualMachineInstanceSpec{
					Domain: kvv1.DomainSpec{
						CPU: &kvv1.CPU{
							Cores: a.opts.CPUs,
						},
						Resources: kvv1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse(a.opts.Memory),
							},
						},
						Devices: kvv1.Devices{
							Disks: disks,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}
}
