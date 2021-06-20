// Copyright 2016 CoreOS, Inc.
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

package misc

import (
	"encoding/base64"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/util"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         NetworkListeners,
		ClusterSize: 1,
		Name:        "fcos.network.listeners",
		Distros:     []string{"fcos"},
		// be sure to notice listeners in the docker stack
		UserData: conf.EmptyIgnition(),
	})
	// TODO: rewrite test for NetworkManager
	register.RegisterTest(&register.Test{
		Run:              NetworkInitramfsSecondBoot,
		ClusterSize:      1,
		Name:             "coreos.network.initramfs.second-boot",
		ExcludePlatforms: []string{"do"},
		ExcludeDistros:   []string{"fcos", "rhcos"},
	})
	// This test follows the same network configuration used on https://github.com/RHsyseng/rhcos-slb
	// with a slight change, where the MCO script is run from ignition: https://github.com/RHsyseng/rhcos-slb/blob/main/setup-ovs.sh.
	register.RegisterTest(&register.Test{
		Run:         NetworkSecondaryNics,
		ClusterSize: 0,
		Name:        "rhcos.network.multipleNics",
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu-unpriv"},
	})
}

type listener struct {
	// udp or tcp; note each v4 variant will also match 'v6'
	protocol string
	port     string
	process  string
}

func checkListeners(c cluster.TestCluster, expectedListeners []listener) error {
	m := c.Machines()[0]

	output := c.MustSSH(m, "sudo ss -plutn")

	processes := strings.Split(string(output), "\n")
	// verify header is as expected
	if len(processes) < 1 {
		c.Fatalf("expected at least one line of ss output: %q", output)
	}
	// ss output's header sometimes does not have whitespace between "Peer Address:Port" and "Process"
	headerRegex := `Netid\s+State\s+Recv-Q\s+Send-Q\s+Local Address:Port\s+Peer Address:Port\s*Process`
	if !regexp.MustCompile(headerRegex).MatchString(processes[0]) {
		c.Fatalf("ss output has changed format: %q", processes[0])
	}
	// skip header
	processes = processes[1:]

	// create expectedListeners map
	expectedListenersMap := map[listener]bool{}
	for _, expected := range expectedListeners {
		expectedListenersMap[expected] = true
	}

NextProcess:
	/*
		Sample `sudo ss -plutn` output:
		Netid  State   Recv-Q  Send-Q  Local Address:Port  Peer Address:Port   Process
		udp    UNCONN  0       0           127.0.0.1:323        0.0.0.0:*      users:(("chronyd",pid=856,fd=5))
		udp    UNCONN  0       0               [::1]:323           [::]:*      users:(("chronyd",pid=856,fd=6))
		tcp    LISTEN  0       128           0.0.0.0:22         0.0.0.0:*      users:(("sshd",pid=1156,fd=5))
		tcp    LISTEN  0       128              [::]:22            [::]:*      users:(("sshd",pid=1156,fd=7))
	*/
	for _, line := range processes {
		parts := strings.Fields(line)
		if len(parts) != 7 {
			c.Fatalf("unexpected number of parts on line: %q in output %q", line, output)
		}
		proto := parts[0]
		portData := strings.Split(parts[4], ":")
		port := portData[len(portData)-1]
		processData := parts[len(parts)-1]
		processStr := regexp.MustCompile(`".+"`).FindString(processData) // process name is captured inside double quotes
		if processStr == "" {
			c.Errorf("%v did not contain program; full output: %q", processData, output)
			continue
		}
		process := processStr[1 : len(processStr)-1]
		thisListener := listener{
			process:  process,
			protocol: proto,
			port:     port,
		}

		if expectedListenersMap[thisListener] {
			// matches expected process
			continue NextProcess
		}

		c.Logf("full ss output: %q", output)
		return fmt.Errorf("Unexpected listener process: %q", line)
	}

	return nil
}

// NetworkListeners checks for listeners with ss.
func NetworkListeners(c cluster.TestCluster) {
	expectedListeners := []listener{
		{"tcp", "22", "sshd"},
		{"udp", "323", "chronyd"},
		// DNS via systemd-resolved
		{"tcp", "53", "systemd-resolve"},
		{"udp", "53", "systemd-resolve"},
		// systemd-resolved also listens on 5355 for Link-Local Multicast Name Resolution
		// https://serverfault.com/a/929642
		{"tcp", "5355", "systemd-resolve"},
		{"udp", "5355", "systemd-resolve"},
	}
	checkList := func() error {
		return checkListeners(c, expectedListeners)
	}
	if err := util.Retry(3, 5*time.Second, checkList); err != nil {
		c.Errorf(err.Error())
	}
}

// NetworkInitramfsSecondBoot verifies that networking is not started in the initramfs on the second boot.
// https://github.com/coreos/bugs/issues/1768
func NetworkInitramfsSecondBoot(c cluster.TestCluster) {
	m := c.Machines()[0]

	if err := m.Reboot(); err != nil {
		c.Errorf("failed to reboot the machine: %v", err)
	}

	// get journal lines from the current boot
	output := c.MustSSH(m, "journalctl -b 0 -o cat -u initrd-switch-root.target -u systemd-networkd.service")
	lines := strings.Split(string(output), "\n")

	// verify that the network service was started
	found := false
	for _, line := range lines {
		if line == "Started Network Service." {
			found = true
			break
		}
	}
	if !found {
		c.Fatal("couldn't find log entry for networkd startup")
	}

	// check that we exited the initramfs first
	if lines[0] != "Reached target Switch Root." {
		c.Fatal("networkd started in initramfs")
	}
}

var (
	defaultLinkConfig = `[Link]
          NamePolicy=mac
          MACAddressPolicy=persistent
`

	dhcpClientConfig = `[main]
          dhcp=dhclient
`

	captureMacsScript = `#!/usr/bin/env bash
          set -ex
          echo "Processing MAC addresses"
          cmdline=( $(</proc/cmdline) )
          karg() {
              local name="$1" value="${2:-}"
              for arg in "${cmdline[@]}"; do
                  if [[ "${arg%%=*}" == "${name}" ]]; then
                      value="${arg#*=}"
                  fi
              done
              echo "${value}"
          }
          udevadm settle
          macs="$(karg macAddressList)"
          if [[ -z $macs ]]; then
            echo "No MAC addresses specified."
            exit 1
          fi
          export PRIMARY_MAC=$(echo $macs | awk -F, '{print $1}')
          export SECONDARY_MAC=$(echo $macs | awk -F, '{print $2}')
          mount "/dev/disk/by-label/boot" /var/mnt
          echo -e "PRIMARY_MAC=${PRIMARY_MAC}\nSECONDARY_MAC=${SECONDARY_MAC}" > /var/mnt/mac_addresses
          umount /var/mnt
	`

	setupOvsScript = `#!/usr/bin/env bash
          set -ex
          if [[ ! -f /boot/mac_addresses ]] ; then
            echo "no mac address configuration file found .. skipping setup-ovs"
            exit 0
          fi
          
          if [[ $(nmcli conn | grep -c ovs) -eq 0 ]]; then
            echo "configure ovs bonding"
            primary_mac=$(cat /boot/mac_addresses | awk -F= '/PRIMARY_MAC/ {print $2}')
            secondary_mac=$(cat /boot/mac_addresses | awk -F= '/SECONDARY_MAC/ {print $2}')
            
            default_device=""
            secondary_device=""
            profile_name=""
            secondary_profile_name=""
            
            
            for dev in $(nmcli device status | awk '/ethernet/ {print $1}'); do
              dev_mac=$(nmcli -g GENERAL.HWADDR dev show $dev | sed -e 's/\\//g' | tr '[A-Z]' '[a-z]')
              case $dev_mac in
                $primary_mac)
                  default_device=$dev
                  profile_name=$(nmcli -g GENERAL.CONNECTION dev show $dev)
                  ;;
                $secondary_mac)
                  secondary_device=$dev
                  secondary_profile_name=$(nmcli -g GENERAL.CONNECTION dev show $dev)
                  ;;
                *)
                  ;;
               esac
            done
            echo -e "default dev: $default_device ($profile_name)\nsecondary dev: $secondary_device ($secondary_profile_name)"
            
            mac=$(sudo nmcli -g GENERAL.HWADDR dev show $default_device | sed -e 's/\\//g')
          
            # delete old bridge if it exists
            ovs-vsctl --if-exists del-br brcnv
            
            # make bridge
            nmcli conn add type ovs-bridge conn.interface brcnv
            nmcli conn add type ovs-port conn.interface brcnv-port master brcnv
            nmcli conn add type ovs-interface \
                           conn.id brcnv-iface \
                           conn.interface brcnv master brcnv-port \
                           ipv4.method auto \
                           ipv4.dhcp-client-id "mac" \
                           connection.autoconnect no \
                           802-3-ethernet.cloned-mac-address $mac
  
            # make bond
            nmcli conn add type ovs-port conn.interface bond0 master brcnv ovs-port.bond-mode balance-slb
            nmcli conn add type ethernet conn.interface $default_device master bond0
            nmcli conn add type ethernet conn.interface $secondary_device master bond0
            nmcli conn down "$profile_name" || true
            nmcli conn mod "$profile_name" connection.autoconnect no || true
            nmcli conn down "$secondary_profile_name" || true
            nmcli conn mod "$secondary_profile_name" connection.autoconnect no || true
            if ! nmcli conn up brcnv-iface; then
                nmcli conn up "$profile_name" || true
                nmcli conn mod "$profile_name" connection.autoconnect yes
                nmcli conn up "$secondary_profile_name" || true
                nmcli conn mod "$secondary_profile_name" connection.autoconnect yes
                nmcli c delete $(nmcli c show |grep ovs-cnv |awk '{print $1}') || true
            else
                nmcli conn mod brcnv-iface connection.autoconnect yes
            fi
          else
              echo "ovs bridge already present"
          fi
`
)

// NetworkSecondaryNics verifies that secondary NICs are created on the node
func NetworkSecondaryNics(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"
	ovsBridgeInterface := "brcnv-iface"

	setupMultipleNetworkTest(c, primaryMac, secondaryMac)

	m := c.Machines()[0]
	checkExpectedMAC(c, m, ovsBridgeInterface, primaryMac)
}

func checkExpectedMAC(c cluster.TestCluster, m platform.Machine, interfaceName, expectedMac string) {
	interfaceMACAddress, err := getInterfaceMAC(c, m, interfaceName)
	if err != nil {
		c.Fatalf("failed to fetch interface %s MAC Address: %v", interfaceName, err)
	}

	if interfaceMACAddress != expectedMac {
		c.Fatalf("interface %s MAC %s does not match expected MAC %s", interfaceName, interfaceMACAddress, expectedMac)
	}
}

func getInterfaceMAC(c cluster.TestCluster, m platform.Machine, interfaceName string) (string, error) {
	output := string(c.MustSSH(m, fmt.Sprintf("nmcli -g 802-3-ethernet.cloned-mac-address connection show %s", interfaceName)))
	output = strings.Replace(output, "\\:", ":", -1)

	var macAddress net.HardwareAddr
	var err error
	if macAddress, err = net.ParseMAC(output); err != nil {
		return "", fmt.Errorf("failed to parse MAC address %v for interface Name %s: %v", output, interfaceName, err)
	}

	return macAddress.String(), nil
}

func checkExpectedIP(c cluster.TestCluster, m platform.Machine, interfaceName, expectedIP string) {
	interfaceIPAddress, err := getInterfaceIP(c, m, interfaceName)
	if err != nil {
		c.Fatalf("failed to fetch bond IP Address: %v", err)
	}

	if interfaceIPAddress != expectedIP {
		c.Fatalf("interface %s IP %s does not match expected IP %s", interfaceName, interfaceIPAddress, expectedIP)
	}
}

func getInterfaceIP(c cluster.TestCluster, m platform.Machine, interfaceName string) (string, error) {
	output := string(c.MustSSH(m, fmt.Sprintf("nmcli -g ip4.address connection show %s", interfaceName)))

	var ipAddress net.IP
	var err error
	if ipAddress, _, err = net.ParseCIDR(output); err != nil {
		return "", fmt.Errorf("failed to parse IP address %v for interface Name %s: %v", output, interfaceName, err)
	}

	return ipAddress.String(), nil
}

func addKernelArgs(c cluster.TestCluster, m platform.Machine, args []string) {
	if len(args) == 0 {
		return
	}

	rpmOstreeCommand := "sudo rpm-ostree kargs"
	for _, arg := range args {
		rpmOstreeCommand = fmt.Sprintf("%s --append %s", rpmOstreeCommand, arg)
	}

	c.MustSSH(m, rpmOstreeCommand)

	err := m.Reboot()
	if err != nil {
		c.Fatalf("failed to reboot the machine: %v", err)
	}
}

func setupMultipleNetworkTest(c cluster.TestCluster, primaryMac, secondaryMac string) {
	var m platform.Machine
	var err error

	options := platform.QemuMachineOptions{
		SecondaryNics: 2,
	}

	var userdata *conf.UserData = conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"files": [
				{
					"path": "/etc/systemd/network/99-default.link",
					"contents": { "source": "data:text/plain;base64,%s" },
					"mode": 420
				},
				{
					"path": "/etc/NetworkManager/conf.d/10-dhcp-config.conf",
					"contents": { "source": "data:text/plain;base64,%s" },
					"mode": 420
				},
				{
					"path": "/usr/local/bin/capture-macs",
					"contents": { "source": "data:text/plain;base64,%s" },
					"mode": 755
				},
				{
					"path": "/usr/local/bin/setup-ovs",
					"contents": { "source": "data:text/plain;base64,%s" },
					"mode": 755
				}
			]
		},
		"systemd": {
			"units": [
				{
					"enabled": true,
					"name": "openvswitch.service"
				},
				{
					"contents": "[Unit]\nDescription=Capture MAC address from kargs\nAfter=network-online.target\nAfter=openvswitch.service\nConditionKernelCommandLine=macAddressList\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/capture-macs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "capture-macs.service"
				},
				{
					"contents": "[Unit]\nDescription=Setup OVS bonding\nAfter=capture-macs.service\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/setup-ovs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "setup-ovs.service"
				}

			]
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte(defaultLinkConfig)),
		base64.StdEncoding.EncodeToString([]byte(dhcpClientConfig)),
		base64.StdEncoding.EncodeToString([]byte(captureMacsScript)),
		base64.StdEncoding.EncodeToString([]byte(setupOvsScript))))

	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *unprivqemu.Cluster:
		m, err = pc.NewMachineWithQemuOptions(userdata, options)
	default:
		panic("unreachable")
	}
	if err != nil {
		c.Fatal(err)
	}

	// Add karg needed for the ignition to configure the network properly.
	addKernelArgs(c, m, []string{fmt.Sprintf("macAddressList=%s,%s", primaryMac, secondaryMac)})
}
