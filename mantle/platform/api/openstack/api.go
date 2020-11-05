// Copyright 2018 Red Hat
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

package openstack

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/floatingips"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/keypairs"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	computeImages "github.com/gophercloud/gophercloud/openstack/compute/v2/images"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/imagedata"
	"github.com/gophercloud/gophercloud/openstack/imageservice/v2/images"
	networkFloatingIPs "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/clientconfig"
	utilsSecurityGroups "github.com/gophercloud/utils/openstack/networking/v2/extensions/security/groups"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/openstack")
)

type Options struct {
	*platform.Options

	// Config file. The path to a clouds.yaml file.
	ConfigPath string
	// Profile name
	Profile string

	// Region (e.g. "regionOne")
	Region string
	// Instance Flavor ID
	Flavor string
	// Image ID
	Image string
	// Network ID
	Network string
	// Domain ID
	Domain string
	// Network to use when creating a Floating IP
	FloatingIPNetwork string
}

type Server struct {
	Server     *servers.Server
	FloatingIP *networkFloatingIPs.FloatingIP
}

type API struct {
	opts          *Options
	computeClient *gophercloud.ServiceClient
	imageClient   *gophercloud.ServiceClient
	networkClient *gophercloud.ServiceClient
}

func New(opts *Options) (*API, error) {
	// The clientconfig library tries to find a clouds.yaml in:
	//     1. OS_CLIENT_CONFIG_FILE
	//     2. Current directory.
	//     3. unix-specific user_config_dir (~/.config/openstack/clouds.yaml)
	//     4. unix-specific site_config_dir (/etc/openstack/clouds.yaml)
	// See https://github.com/gophercloud/utils/blob/8677e053dcf1f05d0fa0a616094aace04690eb94/openstack/clientconfig/utils.go#L93-L112
	//
	// If the user provided a path to a config file set the
	// $OS_CLIENT_CONFIG_FILE env var to it.
	if opts.ConfigPath != "" {
		os.Setenv("OS_CLIENT_CONFIG_FILE", opts.ConfigPath)
	}

	if opts.Profile == "" {
		opts.Profile = "openstack"
	}

	osOpts := &clientconfig.ClientOpts{
		Cloud: opts.Profile,
	}

	if opts.Region != "" {
		osOpts.RegionName = opts.Region
	}

	provider, err := clientconfig.AuthenticatedClient(osOpts)
	if err != nil {
		return nil, fmt.Errorf("failed creating provider: %v", err)
	}

	computeClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Name: "nova",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %v", err)
	}

	imageClient, err := openstack.NewImageServiceV2(provider, gophercloud.EndpointOpts{
		Name: "glance",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create image client: %v", err)
	}

	networkClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Name: "neutron",
	})

	a := &API{
		opts:          opts,
		computeClient: computeClient,
		imageClient:   imageClient,
		networkClient: networkClient,
	}

	if a.opts.Flavor != "" {
		tmp, err := a.resolveFlavor()
		if err != nil {
			return nil, fmt.Errorf("resolving flavor: %v", err)
		}
		a.opts.Flavor = tmp
	}

	if a.opts.Image != "" {
		tmp, err := a.ResolveImage(a.opts.Image)
		if err != nil {
			return nil, fmt.Errorf("resolving image: %v", err)
		}
		a.opts.Image = tmp
	}

	if a.opts.Network != "" {
		tmp, err := a.resolveNetwork(a.opts.Network)
		if err != nil {
			return nil, fmt.Errorf("resolving network: %v", err)
		}
		a.opts.Network = tmp
	}

	return a, nil
}

func unwrapPages(pager pagination.Pager, allowEmpty bool) (pagination.Page, error) {
	if pager.Err != nil {
		return nil, fmt.Errorf("retrieving pager: %v", pager.Err)
	}

	pages, err := pager.AllPages()
	if err != nil {
		return nil, fmt.Errorf("retrieving pages: %v", err)
	}

	if !allowEmpty {
		empty, err := pages.IsEmpty()
		if err != nil {
			return nil, fmt.Errorf("parsing pages: %v", err)
		}
		if empty {
			return nil, fmt.Errorf("empty pager")
		}
	}
	return pages, nil
}

func (a *API) resolveFlavor() (string, error) {
	pager := flavors.ListDetail(a.computeClient, flavors.ListOpts{})

	pages, err := unwrapPages(pager, false)
	if err != nil {
		return "", fmt.Errorf("flavors: %v", err)
	}

	flavors, err := flavors.ExtractFlavors(pages)
	if err != nil {
		return "", fmt.Errorf("extracting flavors: %v", err)
	}

	for _, flavor := range flavors {
		if flavor.ID == a.opts.Flavor || flavor.Name == a.opts.Flavor {
			return flavor.ID, nil
		}
	}

	return "", fmt.Errorf("specified flavor %q not found", a.opts.Flavor)
}

func (a *API) ResolveImage(img string) (string, error) {
	pager := computeImages.ListDetail(a.computeClient, computeImages.ListOpts{})

	pages, err := unwrapPages(pager, false)
	if err != nil {
		return "", fmt.Errorf("images: %v", err)
	}

	images, err := computeImages.ExtractImages(pages)
	if err != nil {
		return "", fmt.Errorf("extracting images: %v", err)
	}

	for _, image := range images {
		if image.ID == img || image.Name == img {
			return image.ID, nil
		}
	}

	return "", fmt.Errorf("specified image %q not found", img)
}

func (a *API) resolveNetwork(network string) (string, error) {
	networks, err := a.getNetworks()
	if err != nil {
		return "", err
	}

	for _, net := range networks {
		if net.ID == network || net.Name == network {
			return net.ID, nil
		}
	}

	return "", fmt.Errorf("specified network %q not found", network)
}

func (a *API) PreflightCheck() error {
	if err := servers.List(a.computeClient, servers.ListOpts{}).Err; err != nil {
		return fmt.Errorf("listing servers: %v", err)
	}
	return nil
}

func (a *API) CreateServer(name, sshKeyID, userdata string) (*Server, error) {
	networkID := a.opts.Network
	if networkID == "" {
		networks, err := a.getNetworks()
		if err != nil {
			return nil, fmt.Errorf("getting network: %v", err)
		}
		networkID = networks[0].ID
	}

	securityGroup, err := a.getSecurityGroup()
	if err != nil {
		return nil, fmt.Errorf("retrieving security group: %v", err)
	}

	server, err := servers.Create(a.computeClient, keypairs.CreateOptsExt{
		CreateOptsBuilder: servers.CreateOpts{
			Name:      name,
			FlavorRef: a.opts.Flavor,
			ImageRef:  a.opts.Image,
			Metadata: map[string]string{
				"CreatedBy": "mantle",
			},
			SecurityGroups: []string{securityGroup},
			Networks: []servers.Network{
				{
					UUID: networkID,
				},
			},
			UserData: []byte(userdata),
		},
		KeyName: sshKeyID,
	}).Extract()
	if err != nil {
		return nil, fmt.Errorf("creating server: %v", err)
	}

	serverID := server.ID

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		server, err = servers.Get(a.computeClient, serverID).Extract()
		if err != nil {
			return false, err
		}
		return server.Status == "ACTIVE", nil
	})
	if err != nil {
		a.DeleteServer(serverID)
		return nil, fmt.Errorf("waiting for instance to run: %v", err)
	}

	var floatingip *networkFloatingIPs.FloatingIP
	if a.opts.FloatingIPNetwork != "" {
		// Create and assign a floating IP to the instance
		floatingip, err = a.createFloatingIP(a.opts.FloatingIPNetwork)
		if err != nil {
			a.DeleteServer(serverID)
			return nil, fmt.Errorf("creating floating ip: %v", err)
		}
		err = floatingips.AssociateInstance(a.computeClient, serverID, floatingips.AssociateOpts{
			FloatingIP: floatingip.FloatingIP,
		}).ExtractErr()
		if err != nil {
			a.DeleteServer(serverID)
			// Explicitly delete the floating ip as DeleteServer only deletes floating IPs that are
			// associated with servers
			a.deleteFloatingIP(floatingip.ID)
			return nil, fmt.Errorf("associating floating ip: %v", err)
		}

		server, err = servers.Get(a.computeClient, serverID).Extract()
		if err != nil {
			a.DeleteServer(serverID)
			return nil, fmt.Errorf("retrieving server info: %v", err)
		}
	}

	return &Server{
		Server:     server,
		FloatingIP: floatingip,
	}, nil
}

func (a *API) getNetworks() ([]networks.Network, error) {
	pager := networks.List(a.networkClient, networks.ListOpts{})

	pages, err := unwrapPages(pager, false)
	if err != nil {
		return nil, fmt.Errorf("networks: %v", err)
	}

	networks, err := networks.ExtractNetworks(pages)
	if err != nil {
		return nil, fmt.Errorf("extracting networks: %v", err)
	}
	return networks, nil
}

func (a *API) getSecurityGroup() (string, error) {
	id, err := utilsSecurityGroups.IDFromName(a.networkClient, "kola")
	if err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			return a.createSecurityGroup()
		}
		return "", fmt.Errorf("finding security group: %v", err)
	}
	return id, nil
}

func (a *API) createSecurityGroup() (string, error) {
	securityGroup, err := groups.Create(a.networkClient, groups.CreateOpts{
		Name: "kola",
	}).Extract()
	if err != nil {
		return "", fmt.Errorf("creating security group: %v", err)
	}

	ruleSet := []struct {
		Direction      rules.RuleDirection
		EtherType      rules.RuleEtherType
		Protocol       rules.RuleProtocol
		PortRangeMin   int
		PortRangeMax   int
		RemoteGroupID  string
		RemoteIPPrefix string
	}{
		{
			Direction:     rules.DirIngress,
			EtherType:     rules.EtherType4,
			RemoteGroupID: securityGroup.ID,
		},
		{
			Direction:      rules.DirIngress,
			EtherType:      rules.EtherType4,
			Protocol:       rules.ProtocolTCP,
			PortRangeMin:   22,
			PortRangeMax:   22,
			RemoteIPPrefix: "0.0.0.0/0",
		},
		{
			Direction:     rules.DirIngress,
			EtherType:     rules.EtherType6,
			RemoteGroupID: securityGroup.ID,
		},
		{
			Direction:      rules.DirIngress,
			EtherType:      rules.EtherType4,
			Protocol:       rules.ProtocolTCP,
			PortRangeMin:   2379,
			PortRangeMax:   2380,
			RemoteIPPrefix: "0.0.0.0/0",
		},
	}

	for _, rule := range ruleSet {
		_, err = rules.Create(a.networkClient, rules.CreateOpts{
			Direction:      rule.Direction,
			EtherType:      rule.EtherType,
			SecGroupID:     securityGroup.ID,
			PortRangeMax:   rule.PortRangeMax,
			PortRangeMin:   rule.PortRangeMin,
			Protocol:       rule.Protocol,
			RemoteGroupID:  rule.RemoteGroupID,
			RemoteIPPrefix: rule.RemoteIPPrefix,
		}).Extract()
		if err != nil {
			a.deleteSecurityGroup(securityGroup.ID)
			return "", fmt.Errorf("adding security rule: %v", err)
		}
	}

	return securityGroup.ID, nil
}

func (a *API) deleteSecurityGroup(id string) error {
	return groups.Delete(a.networkClient, id).ExtractErr()
}

func (a *API) createFloatingIP(network string) (*networkFloatingIPs.FloatingIP, error) {
	networkID, err := a.resolveNetwork(network)
	if err != nil {
		return nil, fmt.Errorf("resolving network: %v", err)
	}
	return networkFloatingIPs.Create(a.networkClient, networkFloatingIPs.CreateOpts{
		FloatingNetworkID: networkID,
	}).Extract()
}

func (a *API) disassociateFloatingIP(serverID, id string) error {
	return floatingips.DisassociateInstance(a.computeClient, serverID, floatingips.DisassociateOpts{
		FloatingIP: id,
	}).ExtractErr()
}

func (a *API) deleteFloatingIP(id string) error {
	return networkFloatingIPs.Delete(a.networkClient, id).ExtractErr()
}

func (a *API) findFloatingIP(serverID string) (*floatingips.FloatingIP, error) {
	pager := floatingips.List(a.computeClient)

	pages, err := unwrapPages(pager, true)
	if err != nil {
		return nil, fmt.Errorf("floating ips: %v", err)
	}

	floatingiplist, err := floatingips.ExtractFloatingIPs(pages)
	if err != nil {
		return nil, fmt.Errorf("extracting floating ips: %v", err)
	}

	for _, floatingip := range floatingiplist {
		if floatingip.InstanceID == serverID {
			return &floatingip, nil
		}
	}

	return nil, nil
}

// Deletes the server, and disassociates & deletes any floating IP associated with the given server.
func (a *API) DeleteServer(id string) error {
	fip, err := a.findFloatingIP(id)
	if err != nil {
		return err
	}
	if fip != nil {
		if err := a.disassociateFloatingIP(id, fip.IP); err != nil {
			return fmt.Errorf("couldn't disassociate floating ip %s from server %s: %v", fip.ID, id, err)
		}
		if err := a.deleteFloatingIP(fip.ID); err != nil {
			// if the deletion of this floating IP fails then mantle cannot detect the floating IP was tied to the
			// server anymore. as such warn and continue deleting the server.
			plog.Warningf("couldn't delete floating ip %s: %v", fip.ID, err)
		}
	}

	if err := servers.Delete(a.computeClient, id).ExtractErr(); err != nil {
		return fmt.Errorf("deleting server: %v: %v", id, err)
	}

	return nil
}

func (a *API) GetConsoleOutput(id string) (string, error) {
	return servers.ShowConsoleOutput(a.computeClient, id, servers.ShowConsoleOutputOpts{}).Extract()
}

func (a *API) UploadImage(name, path string) (string, error) {
	image, err := images.Create(a.imageClient, images.CreateOpts{
		Name:            name,
		ContainerFormat: "bare",
		DiskFormat:      "qcow2",
		Tags:            []string{"mantle"},
	}).Extract()
	if err != nil {
		return "", fmt.Errorf("creating image: %v", err)
	}

	data, err := os.Open(path)
	if err != nil {
		a.DeleteImage(image.ID)
		return "", fmt.Errorf("opening image file: %v", err)
	}
	defer data.Close()

	err = imagedata.Upload(a.imageClient, image.ID, data).ExtractErr()
	if err != nil {
		a.DeleteImage(image.ID)
		return "", fmt.Errorf("uploading image data: %v", err)
	}

	return image.ID, nil
}

func (a *API) DeleteImage(imageID string) error {
	return images.Delete(a.imageClient, imageID).ExtractErr()
}

func (a *API) AddKey(name, key string) error {
	_, err := keypairs.Create(a.computeClient, keypairs.CreateOpts{
		Name:      name,
		PublicKey: key,
	}).Extract()
	return err
}

func (a *API) DeleteKey(name string) error {
	return keypairs.Delete(a.computeClient, name).ExtractErr()
}

func (a *API) listServersWithMetadata(metadata map[string]string) ([]servers.Server, error) {
	pager := servers.List(a.computeClient, servers.ListOpts{})

	pages, err := unwrapPages(pager, true)
	if err != nil {
		return nil, fmt.Errorf("servers: %v", err)
	}

	allServers, err := servers.ExtractServers(pages)
	if err != nil {
		return nil, fmt.Errorf("extracting servers: %v", err)
	}
	var retServers []servers.Server
	for _, server := range allServers {
		isMatch := true
		for key, val := range metadata {
			if value, ok := server.Metadata[key]; !ok || val != value {
				isMatch = false
				break
			}
		}
		if isMatch {
			retServers = append(retServers, server)
		}
	}
	return retServers, nil
}

func (a *API) GC(gracePeriod time.Duration) error {
	threshold := time.Now().Add(-gracePeriod)

	servers, err := a.listServersWithMetadata(map[string]string{
		"CreatedBy": "mantle",
	})
	if err != nil {
		return err
	}
	for _, server := range servers {
		if strings.Contains(server.Status, "DELETED") || server.Created.After(threshold) {
			continue
		}

		if err := a.DeleteServer(server.ID); err != nil {
			return fmt.Errorf("couldn't delete server %s: %v", server.ID, err)
		}
	}
	return nil
}
