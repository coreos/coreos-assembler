package specgen

import (
	"github.com/containers/libpod/libpod/define"
	"github.com/containers/libpod/pkg/rootless"
	"github.com/pkg/errors"
)

var (
	// ErrInvalidPodSpecConfig describes an error given when the podspecgenerator is invalid
	ErrInvalidPodSpecConfig error = errors.New("invalid pod spec")
)

func exclusivePodOptions(opt1, opt2 string) error {
	return errors.Wrapf(ErrInvalidPodSpecConfig, "%s and %s are mutually exclusive pod options", opt1, opt2)
}

// Validate verifies the input is valid
func (p *PodSpecGenerator) Validate() error {
	// PodBasicConfig
	if p.NoInfra {
		if len(p.InfraCommand) > 0 {
			return exclusivePodOptions("NoInfra", "InfraCommand")
		}
		if len(p.InfraImage) > 0 {
			return exclusivePodOptions("NoInfra", "InfraImage")
		}
		if len(p.SharedNamespaces) > 0 {
			return exclusivePodOptions("NoInfra", "SharedNamespaces")
		}
	}

	// PodNetworkConfig
	if err := p.NetNS.validate(); err != nil {
		return err
	}
	if p.NoInfra {
		if p.NetNS.NSMode == NoNetwork {
			return errors.New("NoInfra and a none network cannot be used toegther")
		}
		if p.StaticIP != nil {
			return exclusivePodOptions("NoInfra", "StaticIP")
		}
		if p.StaticMAC != nil {
			return exclusivePodOptions("NoInfra", "StaticMAC")
		}
		if len(p.DNSOption) > 0 {
			return exclusivePodOptions("NoInfra", "DNSOption")
		}
		if len(p.DNSSearch) > 0 {
			return exclusivePodOptions("NoInfo", "DNSSearch")
		}
		if len(p.DNSServer) > 0 {
			return exclusivePodOptions("NoInfra", "DNSServer")
		}
		if len(p.HostAdd) > 0 {
			return exclusivePodOptions("NoInfra", "HostAdd")
		}
		if p.NoManageResolvConf {
			return exclusivePodOptions("NoInfra", "NoManageResolvConf")
		}
	}
	if p.NetNS.NSMode != Bridge {
		if len(p.PortMappings) > 0 {
			return errors.New("PortMappings can only be used with Bridge mode networking")
		}
		if len(p.CNINetworks) > 0 {
			return errors.New("CNINetworks can only be used with Bridge mode networking")
		}
	}
	if p.NoManageResolvConf {
		if len(p.DNSServer) > 0 {
			return exclusivePodOptions("NoManageResolvConf", "DNSServer")
		}
		if len(p.DNSSearch) > 0 {
			return exclusivePodOptions("NoManageResolvConf", "DNSSearch")
		}
		if len(p.DNSOption) > 0 {
			return exclusivePodOptions("NoManageResolvConf", "DNSOption")
		}
	}
	if p.NoManageHosts && len(p.HostAdd) > 0 {
		return exclusivePodOptions("NoManageHosts", "HostAdd")
	}

	if err := p.NetNS.validate(); err != nil {
		return err
	}

	// Set Defaults
	if p.NetNS.Value == "" {
		if rootless.IsRootless() {
			p.NetNS.NSMode = Slirp
		} else {
			p.NetNS.NSMode = Bridge
		}
	}
	if len(p.InfraImage) < 1 {
		p.InfraImage = define.DefaultInfraImage
	}
	if len(p.InfraCommand) < 1 {
		p.InfraCommand = []string{define.DefaultInfraCommand}
	}
	return nil
}
