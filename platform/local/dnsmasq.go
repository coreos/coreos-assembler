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
	"os"
	"os/exec"
	"text/template"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/vishvananda/netlink"
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
	dnsmasq  *exec.Cmd
}

var configTemplate = template.Must(template.New("dnsmasq").Parse(`
keep-in-foreground
leasefile-ro
log-facility=-
pid-file=
quiet-dhcp
quiet-dhcp6
quiet-ra

no-resolv
no-hosts
enable-ra

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
`))

const (
	numInterfaces = 16
	numSegments   = 3
)

func newInterface(s, i byte) *Interface {
	return &Interface{
		HardwareAddr: net.HardwareAddr{0x02, s, 0, 0, 0, i},
		DHCPv4: []net.IPNet{{
			IP:   net.IP{10, s, 0, i},
			Mask: net.CIDRMask(24, 32)}},
		DHCPv6: []net.IPNet{{
			IP:   net.IP{0xfd, s, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, i},
			Mask: net.CIDRMask(64, 128)}},
	}
}

func newSegment(s byte) (*Segment, error) {
	seg := &Segment{
		BridgeName: fmt.Sprintf("br%d", s),
		BridgeIf:   newInterface(s, 1),
	}

	for i := byte(2); i < 2+numInterfaces; i++ {
		seg.Interfaces = append(seg.Interfaces, newInterface(s, i))
	}

	br := netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         seg.BridgeName,
			HardwareAddr: seg.BridgeIf.HardwareAddr,
		},
	}

	if err := netlink.LinkAdd(&br); err != nil {
		return nil, err
	}

	for _, addr := range seg.BridgeIf.DHCPv4 {
		nladdr := netlink.Addr{IPNet: &addr}
		if err := netlink.AddrAdd(&br, &nladdr); err != nil {
			return nil, err
		}
	}

	for _, addr := range seg.BridgeIf.DHCPv6 {
		nladdr := netlink.Addr{IPNet: &addr}
		if err := netlink.AddrAdd(&br, &nladdr); err != nil {
			return nil, err
		}
	}

	if err := netlink.LinkSetUp(&br); err != nil {
		return nil, err
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
	dm.dnsmasq.Stderr = os.Stderr
	cfg, err := dm.dnsmasq.StdinPipe()
	if err != nil {
		return nil, err
	}

	if err = dm.dnsmasq.Start(); err != nil {
		cfg.Close()
		return nil, err
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

func (dm *Dnsmasq) Destroy() error {
	dm.dnsmasq.Process.Kill()
	dm.dnsmasq.Wait()
	return nil
}
