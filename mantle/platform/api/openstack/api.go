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
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/bootfromvolume"
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

	"gopkg.in/yaml.v2"
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

// LoadCloudsYAML defines how to load a clouds.yaml file.
// By default, this calls the local LoadCloudsYAML function.
// See https://github.com/gophercloud/utils/blob/master/openstack/clientconfig/requests.go
func (opts Options) LoadCloudsYAML() (map[string]clientconfig.Cloud, error) {
	// If provided a path to a config file then we load it here.
	if opts.ConfigPath != "" {
		var clouds clientconfig.Clouds
		if content, err := ioutil.ReadFile(opts.ConfigPath); err != nil {
			return nil, err
		} else if err := yaml.Unmarshal(content, &clouds); err != nil {
			return nil, fmt.Errorf("failed to unmarshal yaml %s: %v", opts.ConfigPath, err)
		}
		return clouds.Clouds, nil
	}

	// If not provided a path to a config, fall back to
	// LoadCloudsYAML() from the clientconfig library.
	return clientconfig.LoadCloudsYAML()
}

// LoadSecureCloudsYAML defines how to load a secure.yaml file.
// By default, this calls the local LoadSecureCloudsYAML function.
func (opts Options) LoadSecureCloudsYAML() (map[string]clientconfig.Cloud, error) {
	return clientconfig.LoadSecureCloudsYAML()
}

// LoadPublicCloudsYAML defines how to load a public-secure.yaml file.
// By default, this calls the local LoadPublicCloudsYAML function.
func (opts Options) LoadPublicCloudsYAML() (map[string]clientconfig.Cloud, error) {
	return clientconfig.LoadPublicCloudsYAML()
}

func New(opts *Options) (*API, error) {
	if opts.Profile == "" {
		opts.Profile = "openstack"
	}

	osOpts := &clientconfig.ClientOpts{
		Cloud:    opts.Profile,
		YAMLOpts: opts,
	}

	if opts.Region != "" {
		osOpts.RegionName = opts.Region
	}

	computeClient, err := clientconfig.NewServiceClient("compute", osOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %v", err)
	}

	imageClient, err := clientconfig.NewServiceClient("image", osOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create image client: %v", err)
	}

	networkClient, err := clientconfig.NewServiceClient("network", osOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create network client: %v", err)
	}

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
		uuid, err := a.ResolveImage(a.opts.Image)
		if err != nil {
			return nil, fmt.Errorf("resolving image: %v", err)
		}
		a.opts.Image = uuid
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

	// Define options for the new instance. Use keypairs.CreateOptsExt
	// to add our SSH key to the instance that way.
	serverCreateOpts := keypairs.CreateOptsExt{
		CreateOptsBuilder: servers.CreateOpts{
			Name:      name,
			FlavorRef: a.opts.Flavor,
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
	}
	// Create a boot device volume and create an instance from that by
	// using "boot-from-volume". This means the instances boot a bit faster.
	// Previously we were timing out because it was taking 10+ minutes for
	// instances to come up in VexxHost. This helps with that.
	bootVolume := []bootfromvolume.BlockDevice{
		{
			UUID:                a.opts.Image,
			VolumeSize:          10,
			DeleteOnTermination: true,
			SourceType:          bootfromvolume.SourceImage,
			DestinationType:     bootfromvolume.DestinationVolume,
		},
	}
	server, err := bootfromvolume.Create(a.computeClient, bootfromvolume.CreateOptsExt{
		CreateOptsBuilder: serverCreateOpts,
		BlockDevice:       bootVolume,
	}).Extract()
	if err != nil {
		return nil, fmt.Errorf("creating server: %v", err)
	}

	serverID := server.ID

	err = util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		server, err = servers.Get(a.computeClient, serverID).Extract()
		if err != nil {
			return false, err
		}
		return server.Status == "ACTIVE", nil
	})
	if err != nil {
		if errDelete := a.DeleteServer(serverID); errDelete != nil {
			return nil, fmt.Errorf("deleting server: %v after waiting for instance to run: %v", errDelete, err)
		}
		return nil, fmt.Errorf("waiting for instance to run: %v", err)
	}

	var floatingip *networkFloatingIPs.FloatingIP
	if a.opts.FloatingIPNetwork != "" {
		// Create and assign a floating IP to the instance
		floatingip, err = a.createFloatingIP(a.opts.FloatingIPNetwork)
		if err != nil {
			if errDelete := a.DeleteServer(serverID); errDelete != nil {
				return nil, fmt.Errorf("deleting server: %v after creating floating ip: %v", errDelete, err)
			}
			return nil, fmt.Errorf("creating floating ip: %v", err)
		}
		err = floatingips.AssociateInstance(a.computeClient, serverID, floatingips.AssociateOpts{
			FloatingIP: floatingip.FloatingIP,
		}).ExtractErr()
		if err != nil {
			if errDelete := a.DeleteServer(serverID); errDelete != nil {
				return nil, fmt.Errorf("deleting server: %v after associating floating ip: %v", errDelete, err)
			}
			// Explicitly delete the floating ip as DeleteServer only deletes floating IPs that are
			// associated with servers
			if errDeleteFIP := a.deleteFloatingIP(floatingip.ID); errDeleteFIP != nil {
				return nil, fmt.Errorf("deleting floating ip: %v after associating floating ip: %v", errDeleteFIP, err)
			}
			return nil, fmt.Errorf("associating floating ip: %v", err)
		}

		server, err = servers.Get(a.computeClient, serverID).Extract()
		if err != nil {
			if errDelete := a.DeleteServer(serverID); errDelete != nil {
				return nil, fmt.Errorf("deleting server: %v after retrieving server info: %v", errDelete, err)
			}
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
			if errDelete := a.deleteSecurityGroup(securityGroup.ID); errDelete != nil {
				return "", fmt.Errorf("deleting security group: %v after adding security rule: %v", errDelete, err)
			}
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

func (a *API) UploadImage(name, path, arch, visibility string, protected bool) (string, error) {
	// Get images.ImageVisibility from given visibility string.
	// https://github.com/gophercloud/gophercloud/blob/9cf6777318713a51fbdb1238c19d1213712fd8b4/openstack/imageservice/v2/images/types.go#L52-L68
	var imageVisibility images.ImageVisibility
	switch visibility {
	case "public":
		imageVisibility = images.ImageVisibilityPublic
	case "private":
		imageVisibility = images.ImageVisibilityPrivate
	case "shared":
		imageVisibility = images.ImageVisibilityShared
	case "community":
		imageVisibility = images.ImageVisibilityCommunity
	default:
		return "", fmt.Errorf("Invalid given image visibility: %v", visibility)
	}
	image, err := images.Create(a.imageClient, images.CreateOpts{
		Name:            name,
		ContainerFormat: "bare",
		DiskFormat:      "qcow2",
		Tags:            []string{"mantle"},
		// https://docs.openstack.org/glance/latest/admin/useful-image-properties.html#image-property-keys-and-values
		Properties: map[string]string{"architecture": arch},
		Visibility: &imageVisibility,
		Protected:  &protected,
	}).Extract()
	if err != nil {
		return "", fmt.Errorf("creating image: %v", err)
	}

	data, err := os.Open(path)
	if err != nil {
		if errDelete := a.DeleteImage(image.ID, true); errDelete != nil {
			return "", fmt.Errorf("deleting image: %v after opening image file: %v", errDelete, err)
		}
		return "", fmt.Errorf("opening image file: %v", err)
	}
	defer data.Close()

	err = imagedata.Upload(a.imageClient, image.ID, data).ExtractErr()
	if err != nil {
		if errDelete := a.DeleteImage(image.ID, true); errDelete != nil {
			return "", fmt.Errorf("deleting image: %v after uploading image data: %v", errDelete, err)
		}
		return "", fmt.Errorf("uploading image data: %v", err)
	}

	return image.ID, nil
}

func (a *API) DeleteImage(imageID string, force bool) error {
	// Detect if the image is protected from deletion. If protected
	// and force=true then change protection status and delete it.
	image, err := images.Get(a.imageClient, imageID).Extract()
	if err != nil {
		return err
	}
	if image.Protected {
		if force {
			updateOpts := images.UpdateOpts{
				images.ReplaceImageProtected{
					NewProtected: false,
				},
			}
			_, err = images.Update(a.imageClient, imageID, updateOpts).Extract()
			if err != nil {
				return fmt.Errorf(
					"Error removing protection from image %s: %v", imageID, err)
			}
		} else {
			return fmt.Errorf(
				"Image %s is protected from deletion and force is not enabled", imageID)
		}
	}
	// Finally, delete the image.
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
	return keypairs.Delete(a.computeClient, name, nil).ExtractErr()
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
