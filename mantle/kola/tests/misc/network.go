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
		Run:         NetworkAdditionalNics,
		ClusterSize: 0,
		Name:        "rhcos.network.multiple-nics",
		Timeout:     20 * time.Minute,
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu-unpriv"},
	})
	// This test follows the same network configuration used on https://github.com/RHsyseng/rhcos-slb
	// with a slight change, where the MCO script is run from ignition: https://github.com/RHsyseng/rhcos-slb/blob/main/setup-ovs.sh.
	// and we're using veth pairs instead of real nic when setting the bond
	register.RegisterTest(&register.Test{
		Run:         NetworkBondWithDhcp,
		ClusterSize: 0,
		Name:        "rhcos.network.bond-with-dhcp",
		Timeout:     20 * time.Minute,
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu-unpriv"},
	})
	// This test follows the same network configuration used on https://github.com/RHsyseng/rhcos-slb
	// with a slight change, where the MCO script is run from ignition: https://github.com/RHsyseng/rhcos-slb/blob/main/setup-ovs.sh.
	// and we're using veth pairs instead of real nic when setting the bond
	register.RegisterTest(&register.Test{
		Run:         NetworkBondWithRestart,
		ClusterSize: 0,
		Name:        "rhcos.network.bond-with-restart",
		Timeout:     20 * time.Minute,
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
          mount -o rw,remount /boot
          echo -e "PRIMARY_MAC=${PRIMARY_MAC}\nSECONDARY_MAC=${SECONDARY_MAC}" > /boot/mac_addresses
	`

	setupVethPairsTemplate = `#!/usr/bin/env bash
          set -ex

          create_veth_pair() {
            veth_host_side_end_name=$1
            veth_netns_side_end_name=$2
            host_side_mac_address=$3
            netns_side_ip=$4
            network_namespace=$5

            # Create veth pair and assign a namespace to veth-netns
            ip link add ${veth_host_side_end_name} type veth peer name ${veth_netns_side_end_name}
            ip link set ${veth_netns_side_end_name} netns ${network_namespace}

            # Assign an MAC address to the host-side veth end
            ip link set dev ${veth_host_side_end_name} address ${host_side_mac_address}

            # Assign an IP address to netns-side veth end and bring it up
            ip netns exec ${network_namespace} ip address add ${netns_side_ip} dev ${veth_netns_side_end_name}
            ip netns exec ${network_namespace} ip link set ${veth_netns_side_end_name} up
          }

          activate_veth_end() {
            veth_end_name=$1

            nmcli dev set ${veth_end_name} managed yes
            ip link set ${veth_end_name} up

            poll_dhcp_success ${veth_end_name}
          }

          poll_dhcp_success() {
            veth_end_name=$1
            for attempt in {1..5}; do
              cidr=$(nmcli -g IP4.ADDRESS dev show ${veth_end_name})
              if [[ ! -z "${cidr}" ]]; then
                return
              fi
              sleep 1
            done

            echo "failed to get ip for ${veth_end_name}"
            exit 1
          }

          primary_mac=%s
          secondary_mac=%s
          primary_ip=%s
          secondary_ip=%s

          # Get the location of the NetworkManager config files
          NMPATH=$(NetworkManager --print-config | sed -nr "/^\[keyfile\]/ { :l /^path[ ]*/ { s/.*=[ ]*//; p; q;}; n; b l;}")

          # Store the location of the NM config files
          if [[ ! -d $NMPATH ]]; then
           NMPATH=/etc/NetworkManager/system-connections
          fi
          rm -rf ${NMPATH}/*

          if [[ ! -f /boot/mac_addresses ]] ; then
            echo "no mac address configuration file found .. exiting"
            exit 0
          fi

          network_namespace=test-netns
          # Create a network namespace
          ip netns add ${network_namespace}

          # create 2 veth pairs between host side and network_namespace
          veth1_host_side_end_name=veth1-host
          veth1_netns_side_end_name=veth1-netns
          create_veth_pair ${veth1_host_side_end_name} ${veth1_netns_side_end_name} ${primary_mac} 192.168.0.100 ${network_namespace}
          veth2_host_side_end_name=veth2-host
          veth2_netns_side_end_name=veth2-netns
          create_veth_pair ${veth2_host_side_end_name} ${veth2_netns_side_end_name} ${secondary_mac} 192.168.0.101 ${network_namespace}

          # Run a dnsmasq service on the network_namespace, to set the host-side veth ends a ip via their MAC addresses
          echo -e "dhcp-range=192.168.0.50,192.168.0.60,255.255.255.0,12h\ndhcp-host=${primary_mac},${primary_ip}\ndhcp-host=${secondary_mac},${secondary_ip}" > /etc/dnsmasq.d/dhcp
          ip netns exec ${network_namespace} dnsmasq &

          # Tell NM to manage the "veth-host" interface and bring it up (will attempt DHCP).
          # Do this after we start dnsmasq so we don't have to deal with DHCP timeouts.
          activate_veth_end ${veth1_host_side_end_name}
          activate_veth_end ${veth2_host_side_end_name}

          # Run setup Ovs script to create the ovs bridge over the bond, using the 2 host-side veth ends.
          /usr/local/bin/setup-ovs
`

	setupOvsScript = `#!/usr/bin/env bash
          set -ex

          rm -rf /etc/NetworkManager/system-connections/*

          # Get the location of the NetworkManager config files
          NMPATH=$(NetworkManager --print-config | sed -nr "/^\[keyfile\]/ { :l /^path[ ]*/ { s/.*=[ ]*//; p; q;}; n; b l;}")

          if [[ ! -f /boot/mac_addresses ]] ; then
            echo "no mac address configuration file found .. exiting"
            exit 1
          fi

          # Store the location of the NM config files
          if [[ ! -d $NMPATH ]]; then
           NMPATH=/etc/NetworkManager/system-connections
          fi
          
          # Copy the config files from stored location to NM settings folder
          if [[ -d /var/ovsbond ]]; then
            echo "Loading OVS old profile"
            cp -r /var/ovsbond/*  $NMPATH
            systemctl restart NetworkManager
          fi

          if [[ $(nmcli conn | grep -c ovs) -eq 0 ]]; then
            echo "configure ovs bonding"
            ovs-vsctl --if-exists del-br brcnv
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

            # make bridge
            nmcli conn add type ovs-bridge conn.interface brcnv con-name ovs-bridge-brcnv
            nmcli conn add type ovs-port conn.interface brcnv-port master brcnv
            nmcli conn add type ovs-interface con-name ovs-brcnv-iface conn.interface brcnv master brcnv-port ipv4.method auto ipv4.dhcp-client-id "mac" connection.autoconnect no 802-3-ethernet.cloned-mac-address $mac

            # make bond
            nmcli conn add type ovs-port conn.interface bond0 master brcnv ovs-port.bond-mode balance-slb con-name ovs-slave-bond0
            nmcli conn add type ethernet conn.interface $default_device master bond0 con-name ovs-slave-1
            nmcli conn add type ethernet conn.interface $secondary_device master bond0 con-name ovs-slave-2
            nmcli conn down "$profile_name" || true
            nmcli conn mod "$profile_name" connection.autoconnect no || true
            nmcli conn down "$secondary_profile_name" || true
            nmcli conn mod "$secondary_profile_name" connection.autoconnect no || true
            if ! nmcli conn up ovs-brcnv-iface; then
          	  nmcli conn up "$profile_name" || true
          	  nmcli conn mod "$profile_name" connection.autoconnect yes
          	  nmcli conn up "$secondary_profile_name" || true
          	  nmcli conn mod "$secondary_profile_name" connection.autoconnect yes
          	  nmcli conn delete $(nmcli c show |grep ovs-cnv |awk '{print $1}') || true
            else
          	  nmcli conn mod ovs-brcnv-iface connection.autoconnect yes
          	  nmcli conn up ovs-slave-2
          	  # Remove Old NM config files and copy the new-ones
          	  rm -rf /var/ovsbond
          	  cp -r $NMPATH /var/ovsbond || true
            fi
          else
          	echo "ovs bridge already present"
          	for c in ovs-bridge-brcnv ovs-slave-bond0 ovs-slave-brcnv-port ovs-slave-1 ovs-slave-2 ovs-brcnv-iface; do nmcli c up $c; done
          fi
`
)

func NetworkBondWithRestart(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"
	primaryIp := "192.168.0.55"
	secondaryIp := "192.168.0.56"

	setupBondWithDhcpTest(c, primaryMac, secondaryMac, primaryIp, secondaryIp)

	m := c.Machines()[0]
	expectedUpConnections := getOvsRelatedConnections(c, m)
	err := checkConnectionsUp(c, m, expectedUpConnections)
	if err != nil {
		c.Fatalf("connections check failed before reboot: %v", err)
	}

	err = m.Reboot()
	if err != nil {
		c.Fatalf("failed to reboot the machine: %v", err)
	}

	err = checkConnectionsUp(c, m, expectedUpConnections)
	if err != nil {
		c.Fatalf("connections check failed post reboot: %v", err)
	}
}

func getOvsRelatedConnections(c cluster.TestCluster, m platform.Machine) []string {
	subString := "ovs"
	connectionList := getConnectionsList(c, m)
	ovsRelatedConnectionList := []string{}

	for _, connectionName := range connectionList {
		if strings.Contains(connectionName, subString) {
			ovsRelatedConnectionList = append(ovsRelatedConnectionList, connectionName)
		}
	}
	return ovsRelatedConnectionList
}

func getConnectionsList(c cluster.TestCluster, m platform.Machine) []string {
	output := string(c.MustSSH(m, "nmcli -t -g NAME con show"))
	return strings.Split(output, "\n")
}

func getActiveConnectionsList(c cluster.TestCluster, m platform.Machine) []string {
	output := string(c.MustSSH(m, "nmcli -t -g NAME con show --active"))
	return strings.Split(output, "\n")
}

func NetworkBondWithDhcp(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"
	primaryIp := "192.168.0.55"
	secondaryIp := "192.168.0.56"
	ovsBridgeInterface := "ovs-brcnv-iface"

	setupBondWithDhcpTest(c, primaryMac, secondaryMac, primaryIp, secondaryIp)

	m := c.Machines()[0]
	checkExpectedMAC(c, m, ovsBridgeInterface, primaryMac)
	checkExpectedIP(c, m, ovsBridgeInterface, primaryIp)
}

func initSetupVethPairsScript(primaryMac, secondaryMac, primaryIp, secondaryIp string) string {
	return fmt.Sprintf(setupVethPairsTemplate, primaryMac, secondaryMac, primaryIp, secondaryIp)
}

func setupBondWithDhcpTest(c cluster.TestCluster, primaryMac, secondaryMac, primaryIp, secondaryIp string) {
	var m platform.Machine
	var err error
	options := platform.QemuMachineOptions{}

	setupVethPairs := initSetupVethPairsScript(primaryMac, secondaryMac, primaryIp, secondaryIp)

	var userdata *conf.UserData = conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"files": [
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
					"path": "/usr/local/bin/create-veth-pairs",
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
					"contents": "[Unit]\nDescription=Capture MAC address from kargs\nBefore=coreos-installer.target\nAfter=coreos-installer.service\n\nConditionKernelCommandLine=macAddressList\nRequiresMountsFor=/boot\n\n[Service]\nType=oneshot\nMountFlags=slave\nExecStart=/usr/local/bin/capture-macs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "capture-macs.service"
				},
				{
					"contents": "[Unit]\nDescription=Create VETH Pairs and Configue OVS interface over bond\nBefore=ovs-configuration.service\nAfter=NetworkManager.service\nAfter=openvswitch.service\nAfter=capture-macs.service\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/create-veth-pairs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "create-veth-pairs.service"
				}
			]
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte(dhcpClientConfig)),
		base64.StdEncoding.EncodeToString([]byte(captureMacsScript)),
		base64.StdEncoding.EncodeToString([]byte(setupVethPairs)),
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

// NetworkAdditionalNics verifies that additional NICs are created on the node
func NetworkAdditionalNics(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"
	ovsBridgeInterface := "ovs-brcnv-iface"

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

func checkConnectionsUp(c cluster.TestCluster, m platform.Machine, expectedUpConnections []string) error {
	failedConnections := []string{}
	activeConnections := getActiveConnectionsList(c, m)

	for _, connection := range expectedUpConnections {
		if !isStringInSlice(activeConnections, connection) {
			failedConnections = append(failedConnections, connection)
		}
	}
	if len(failedConnections) != 0 {
		return fmt.Errorf("connections not in expected status up: %v", failedConnections)
	}
	return nil
}

func isStringInSlice(stringList []string, val string) bool {
	for _, str := range stringList {
		if str == val {
			return true
		}
	}
	return false
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

	c.RunCmdSync(m, rpmOstreeCommand)

	err := m.Reboot()
	if err != nil {
		c.Fatalf("failed to reboot the machine: %v", err)
	}
}

func setupMultipleNetworkTest(c cluster.TestCluster, primaryMac, secondaryMac string) {
	var m platform.Machine
	var err error

	options := platform.QemuMachineOptions{
		MachineOptions: platform.MachineOptions{
			AdditionalNics: 2,
		},
	}

	var userdata *conf.UserData = conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"files": [
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
					"contents": "[Unit]\nDescription=Capture MAC address from kargs\nBefore=coreos-installer.target\nAfter=coreos-installer.service\n\nConditionKernelCommandLine=macAddressList\nRequiresMountsFor=/boot\n\n[Service]\nType=oneshot\nMountFlags=slave\nExecStart=/usr/local/bin/capture-macs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "capture-macs.service"
				},
				{
					"contents": "[Unit]\nDescription=Setup OVS bonding\nBefore=ovs-configuration.service\nAfter=NetworkManager.service\nAfter=openvswitch.service\nAfter=capture-macs.service\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/setup-ovs\n\n[Install]\nRequiredBy=multi-user.target\n",
					"enabled": true,
					"name": "setup-ovs.service"
				}

			]
		}
	}`,
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
