// Copyright 2015 CoreOS, Inc.
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

package local

import (
	"fmt"
	"net"
	"text/template"

	"github.com/coreos/pkg/capnslog"
	"github.com/vishvananda/netlink"

	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
)

type Interface struct {
	HardwareAddr net.HardwareAddr
	DHCPv4       []net.IPNet
	DHCPv6       []net.IPNet
	//SLAAC net.IPAddr
}

type Segment struct {
	BridgeName string
	BridgeIf   *Interface
	Interfaces []*Interface
	nextIf     int
}

type Dnsmasq struct {
	Segments []*Segment
	dnsmasq  *exec.ExecCmd
}

const (
	numInterfaces = 500 // affects dnsmasq startup time
	numSegments   = 1

	debugConfig = `
log-queries
log-dhcp
`

	quietConfig = `
quiet-dhcp
quiet-dhcp6
quiet-ra
`

	commonConfig = `
keep-in-foreground
leasefile-ro
log-facility=-
pid-file=

no-resolv
no-hosts
enable-ra

# point NTP at this host (0.0.0.0 and :: are special)
dhcp-option=option:ntp-server,0.0.0.0
dhcp-option=option6:ntp-server,[::]

{{range .Segments}}
domain={{.BridgeName}}.local

{{range .BridgeIf.DHCPv4}}
dhcp-range={{.IP}},static
{{end}}

{{range .BridgeIf.DHCPv6}}
dhcp-range={{.IP}},ra-names,slaac
{{end}}

{{range .Interfaces}}
dhcp-host={{.HardwareAddr}}{{template "ips" .DHCPv4}}{{template "ips" .DHCPv6}}
{{end}}
{{end}}

{{define "ips"}}{{range .}}{{printf ",%s" .IP}}{{end}}{{end}}
`
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/local")

func newInterface(s byte, i uint16) *Interface {
	return &Interface{
		HardwareAddr: net.HardwareAddr{0x02, s, 0, 0, byte(i / 256), byte(i % 256)},
		DHCPv4: []net.IPNet{{
			IP:   net.IP{10, s, byte(i / 256), byte(i % 256)},
			Mask: net.CIDRMask(16, 32)}},
		DHCPv6: []net.IPNet{{
			IP:   net.IP{0xfd, s, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(i / 256), byte(i % 256)},
			Mask: net.CIDRMask(64, 128)}},
	}
}

func newSegment(s byte) (*Segment, error) {
	seg := &Segment{
		BridgeName: fmt.Sprintf("br%d", s),
		BridgeIf:   newInterface(s, 1),
	}

	for i := uint16(2); i < 2+numInterfaces; i++ {
		seg.Interfaces = append(seg.Interfaces, newInterface(s, i))
	}

	br := netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         seg.BridgeName,
			HardwareAddr: seg.BridgeIf.HardwareAddr,
		},
	}

	if err := netlink.LinkAdd(&br); err != nil {
		return nil, fmt.Errorf("LinkAdd() failed: %v", err)
	}

	for _, addr := range seg.BridgeIf.DHCPv4 {
		nladdr := netlink.Addr{IPNet: &addr}
		if err := netlink.AddrAdd(&br, &nladdr); err != nil {
			return nil, fmt.Errorf("DHCPv4 AddrAdd() failed: %v", err)
		}
	}

	for _, addr := range seg.BridgeIf.DHCPv6 {
		nladdr := netlink.Addr{IPNet: &addr}
		if err := netlink.AddrAdd(&br, &nladdr); err != nil {
			return nil, fmt.Errorf("DHCPv6 AddrAdd() failed: %v", err)
		}
	}

	if err := netlink.LinkSetUp(&br); err != nil {
		return nil, fmt.Errorf("LinkSetUp() failed: %v", err)
	}

	return seg, nil
}

func NewDnsmasq() (*Dnsmasq, error) {
	dm := &Dnsmasq{}
	for s := byte(0); s < numSegments; s++ {
		seg, err := newSegment(s)
		if err != nil {
			return nil, fmt.Errorf("Network setup failed: %v", err)
		}
		dm.Segments = append(dm.Segments, seg)
	}

	// setup lo
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return nil, fmt.Errorf("Network loopback setup failed: %v", err)
	}
	err = netlink.LinkSetUp(lo)
	if err != nil {
		return nil, fmt.Errorf("Network loopback setup failed: %v", err)
	}

	dm.dnsmasq = exec.Command("dnsmasq", "--conf-file=-")
	cfg, err := dm.dnsmasq.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := dm.dnsmasq.StdoutPipe()
	if err != nil {
		return nil, err
	}
	dm.dnsmasq.Stderr = dm.dnsmasq.Stdout
	go util.LogFrom(capnslog.INFO, out)

	if err = dm.dnsmasq.Start(); err != nil {
		cfg.Close()
		return nil, err
	}

	var configTemplate *template.Template

	if plog.LevelAt(capnslog.DEBUG) {
		configTemplate = template.Must(
			template.New("dnsmasq").Parse(debugConfig + commonConfig))
	} else {
		configTemplate = template.Must(
			template.New("dnsmasq").Parse(quietConfig + commonConfig))
	}

	if err = configTemplate.Execute(cfg, dm); err != nil {
		cfg.Close()
		dm.Destroy()
		return nil, err
	}
	cfg.Close()

	return dm, nil
}

func (dm *Dnsmasq) GetInterface(bridge string) (in *Interface) {
	for _, seg := range dm.Segments {
		if bridge == seg.BridgeName {
			if seg.nextIf >= len(seg.Interfaces) {
				panic("Not enough interfaces!")
			}
			in = seg.Interfaces[seg.nextIf]
			seg.nextIf++
			return
		}
	}
	panic("Not a valid bridge!")
}

func (dm *Dnsmasq) Destroy() {
	if err := dm.dnsmasq.Kill(); err != nil {
		plog.Errorf("Error killing dnsmasq: %v", err)
	}
}
