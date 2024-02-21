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

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         NetworkListeners,
		ClusterSize: 1,
		Name:        "fcos.network.listeners",
		Description: "Verify the nework listeners are expected.",
		Distros:     []string{"fcos"},
		// be sure to notice listeners in the docker stack
		UserData: conf.EmptyIgnition(),
	})
	// TODO: rewrite test for NetworkManager
	register.RegisterTest(&register.Test{
		Run:            NetworkInitramfsSecondBoot,
		ClusterSize:    1,
		Name:           "coreos.network.initramfs.second-boot",
		Description:    "Verify that networking is not started in the initramfs on the second boot.",
		ExcludeDistros: []string{"fcos", "rhcos"},
	})
	// This test follows the same network configuration used on https://github.com/RHsyseng/rhcos-slb
	register.RegisterTest(&register.Test{
		Run:         NetworkAdditionalNics,
		ClusterSize: 0,
		Name:        "rhcos.network.multiple-nics",
		Description: "Verify configuring networking with multiple NICs work.",
		Timeout:     20 * time.Minute,
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu"},
	})
	// This test follows the same network configuration used on https://github.com/RHsyseng/rhcos-slb
	// with a slight change, where the script originally run by MCO is run from
	// ignition: https://github.com/RHsyseng/rhcos-slb/blob/161a421f8fdcdb4b08fb6366daa8fe1b75cbe095/init-interfaces.sh.
	register.RegisterTest(&register.Test{
		Run:         InitInterfacesTest,
		ClusterSize: 0,
		Name:        "rhcos.network.init-interfaces-test",
		Description: "Verify init-interfaces script works in both fresh setup and reboot.",
		Timeout:     40 * time.Minute,
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu"},
		Tags:        []string{"openshift"},
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
		// DHCPv6 from NetworkManager (when IPv6 network available)
		// https://github.com/coreos/fedora-coreos-tracker/issues/1216
		{"udp", "546", "NetworkManager"},
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
	// copied from https://github.com/RHsyseng/rhcos-slb/blob/31788956cc663d8375d7b8c09df015e623c7afb3/capture-macs.sh
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
	# Wait for device nodes
	udevadm settle

	macs="$(karg macAddressList)"
	if [[ -z $macs ]]; then
		echo "No MAC addresses specified."
		exit 0
	fi

	export PRIMARY_MAC=$(echo $macs | awk -F, '{print $1}')
	export SECONDARY_MAC=$(echo $macs | awk -F, '{print $2}')
	mount -o rw,remount /boot
	echo -e "PRIMARY_MAC=${PRIMARY_MAC}\nSECONDARY_MAC=${SECONDARY_MAC}" > /boot/mac_addresses
	`

	// copied from https://github.com/RHsyseng/rhcos-slb/blob/31788956cc663d8375d7b8c09df015e623c7afb3/init-interfaces.sh
	initInterfacesScript = `#!/usr/bin/env bash
	set -ex
	is_configured() {
	  [[ $(grep primary /etc/NetworkManager/system-connections/* | wc -l) -ge 1 && $(grep secondary /etc/NetworkManager/system-connections/* | wc -l) -ge 1 ]]
	}

	is_con_exists() {
	  local con_name=$1
	  if nmcli -t -g NAME con show | grep -w -q "$con_name"; then
		return 0 # true
	  fi
	  return 1 # false
	}

	is_con_active() {
	  local con_name=$1
	  if nmcli -t -g NAME con show --active | grep -w -q "$con_name"; then
		return 0 # true
	  fi
	  return 1 # false
	}

	get_con_name_by_mac_or_device() {
	  local mac=$(echo $1 | sed -e 's/\\\|://g')
	  local dev_name=$2
	  while read -r con; do
		if [[ "$(nmcli -g 802-3-ethernet.mac-address c show "${con}" | tr '[A-Z]' '[a-z]' | sed -e 's/\\\|://g')" == "$mac" || $(nmcli -g connection.interface-name c show "${con}") == "${dev_name}" ]]; then
		  echo "${con}"
		  break
		fi
	  done <<< "$(nmcli -g NAME c show)"
	}

	generate_new_con_name() {
	  local device_name=$1
	  printf "ethernet-%s-%s" "$device_name" "$RANDOM"
	}

	set_description() {
	  local mac=$1
	  local nic=$2
	  local description=$3
	  local connections=$(grep -REl "type=ethernet" /etc/NetworkManager/system-connections | xargs -I{} -- grep -El -i "mac-address=${mac}|interface-name=${nic}" "{}")
	  IFS=$'\n'
	  for connection in ${connections}; do
		  if ! grep nmstate.interface.description "${connection}"; then
			echo "" >> "${connection}"
			echo "[user]" >> "${connection}"
			echo "nmstate.interface.description=${description}" >> "${connection}"
		  else
			sed -i "s/nmstate\.interface\.description=.*/nmstate.interface.description=$description/" "${connection}"
		  fi
	  done
	  unset IFS
	}

	if [[ ! -f /boot/mac_addresses ]] ; then
	  echo "no mac address configuration file found .. exiting"
	  exit 0
	fi

	if is_configured; then
	  echo "interfaces already configured"
	  exit 0
	fi

	primary_mac="$(awk -F= '/PRIMARY_MAC/ {print $2}' < /boot/mac_addresses | tr '[:upper:]' '[:lower:]')"
	secondary_mac="$(awk -F= '/SECONDARY_MAC/ {print $2}' < /boot/mac_addresses | tr '[:upper:]' '[:lower:]')"

	default_device=""
	secondary_device=""
	default_connection_name=""
	secondary_connection_name=""

	for dev in $(nmcli device status | awk '/ethernet/ {print $1}'); do
	  dev_mac=$(nmcli -g GENERAL.HWADDR dev show "$dev" | sed -e 's/\\//g' | tr '[:upper:]' '[:lower:]')
	  case $dev_mac in
		"${primary_mac}")
		  default_device="$dev"
		  default_connection_name=$(get_con_name_by_mac_or_device "$primary_mac" "$dev")
		  ;;
		"${secondary_mac}")
		  secondary_device="$dev"
		  secondary_connection_name=$(get_con_name_by_mac_or_device "$secondary_mac" "$dev")
		  ;;
		*)
		  ;;
	  esac
	done

	echo -e "default dev: $default_device (CONNECTION.NAME $default_connection_name)\nsecondary dev: $secondary_device (CONNECTION.NAME $secondary_connection_name)"
	if [[ -z "$default_device" ]] || [[ -z "$secondary_device" ]]; then
	  echo "error: primary/secondary device name not found"
	  exit 0
	fi

	if eval ! is_con_exists "\"$default_connection_name\""; then
	  default_connection_name="$(generate_new_con_name "${default_device}")" && export default_connection_name
	  nmcli con add type ethernet \
					conn.interface "${default_device}" \
					connection.autoconnect yes \
					ipv4.method auto \
					con-name "${default_connection_name}" \
					802-3-ethernet.mac-address "${primary_mac}"
	fi
	if eval ! is_con_active "\"$default_connection_name\""; then
	  nmcli con up "${default_connection_name}"
	fi

	if eval ! is_con_exists "\"$secondary_connection_name\""; then
	  secondary_connection_name="$(generate_new_con_name "${secondary_device}")" && export secondary_connection_name
	  nmcli con add type ethernet \
					conn.interface "${secondary_device}" \
					connection.autoconnect yes \
					ipv4.method disabled \
					ipv6.method disabled \
					con-name "${secondary_connection_name}" \
					802-3-ethernet.mac-address "${secondary_mac}"
	fi
	if eval ! is_con_active "\"${secondary_connection_name}\""; then
	  nmcli con mod "${secondary_connection_name}" \
					connection.interface-name "${secondary_device}" \
					connection.autoconnect yes \
					ipv4.method disabled \
					ipv6.method disabled \
					802-3-ethernet.mac-address "${secondary_mac}"
	  nmcli con up "${secondary_connection_name}" || /bin/true
	fi

	set_description "${primary_mac}" "${default_device}" primary
	set_description "${secondary_mac}" "${secondary_device}" secondary

	nmcli c reload
	`

	initInterfacesSystemdContents = `
[Unit]
Before=kubelet.service
After=NetworkManager.service capture-macs.service
[Service]
Type=oneshot
ExecStart=/bin/sh /var/init-interfaces.sh
[Install]
WantedBy=multi-user.target
`

	captureMacsSystemdContents = `
[Unit]
RequiresMountsFor=/boot
Description=Capture MAC address from kargs
After=create-datastore.service
Before=coreos-installer.target
[Service]
Type=oneshot
MountFlags=slave
ExecStart=/usr/local/bin/capture-macs
[Install]
RequiredBy=multi-user.target
`
)

// NetworkAdditionalNics verifies that additional NICs are created on the node
func NetworkAdditionalNics(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"

	setupMultipleNetworkTest(c, primaryMac, secondaryMac)

	m := c.Machines()[0]
	expectedMacsList := []string{primaryMac, secondaryMac}
	checkExpectedMACs(c, m, expectedMacsList)
}

// InitInterfacesTest checks init-interfaces script in both fresh setup and Node reboot
// In this test MCO is checked as part of ignition context since MCO is not available on this test infra
func InitInterfacesTest(c cluster.TestCluster) {
	primaryMac := "52:55:00:d1:56:00"
	secondaryMac := "52:55:00:d1:56:01"

	setupWithInterfacesTest(c, primaryMac, secondaryMac)
	m := c.Machines()[0]
	err := simulateNewInstallation(c, m, []string{primaryMac, secondaryMac})
	if err != nil {
		c.Fatalf("failed to simulate new setup with no connections: %v", err)
	}
	err = checkExpectedInterfaces(c, m, primaryMac, secondaryMac)
	if err != nil {
		c.Fatalf("failed when no connections are configured: %v", err)
	}

	err = m.Reboot()
	if err != nil {
		c.Fatalf("failed to reboot Node: %v", err)
	}
	err = checkExpectedInterfaces(c, m, primaryMac, secondaryMac)
	if err != nil {
		c.Fatalf("failed when after setup reboot: %v", err)
	}
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

	var userdata = conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"files": [
			  {
				"path": "/usr/local/bin/capture-macs",
				"contents": { "source": "data:text/plain;base64,%s" },
				"mode": 755
			  }
			]
		},
		"systemd": {
			"units": [
			  {
				"contents": "[Unit]\nRequiresMountsFor=/boot\nDescription=Capture MAC address from kargs\nAfter=create-datastore.service\nBefore=coreos-installer.target\n\n\n[Service]\nType=oneshot\nMountFlags=slave\nExecStart=/usr/local/bin/capture-macs\n\n[Install]\nRequiredBy=multi-user.target\n",
				"enabled": true,
				"name": "capture-macs.service"
			  }
			]
		}
	}`, base64.StdEncoding.EncodeToString([]byte(captureMacsScript))))

	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *qemu.Cluster:
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

func setupWithInterfacesTest(c cluster.TestCluster, primaryMac, secondaryMac string) {
	var userdata = conf.Ignition(fmt.Sprintf(`{
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
					"path": "/var/init-interfaces.sh",
					"contents": { "source": "data:text/plain;base64,%s" },
					"mode": 755
				}
			]
		},
		"systemd": {
			"units": [
				{
					"contents": "%s",
					"enabled": true,
					"name": "capture-macs.service"
				},
				{
					"contents": "%s",
					"enabled": true,
					"name": "setup-ovs.service"
				}
			]
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte(captureMacsScript)),
		base64.StdEncoding.EncodeToString([]byte(initInterfacesScript)),
		strings.Replace(captureMacsSystemdContents, "\n", "\\n", -1),
		strings.Replace(initInterfacesSystemdContents, "\n", "\\n", -1)))

	options := platform.QemuMachineOptions{
		MachineOptions: platform.MachineOptions{
			AdditionalNics: 2,
		},
	}

	var m platform.Machine
	var err error
	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *qemu.Cluster:
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

func simulateNewInstallation(c cluster.TestCluster, m platform.Machine, macConnectionsToDelete []string) error {
	macConnectionMap, err := getMacConnectionMap(c, m)
	if err != nil {
		return fmt.Errorf("failed to get macConnectionMap: %v", err)
	}
	macInterfaceMap, err := getMacInterfaceMap(c, m)
	if err != nil {
		return fmt.Errorf("failed to get macInterfaceMap: %v", err)
	}
	flushInterfaceIpByMac(c, m, macInterfaceMap, macConnectionsToDelete)
	removeConnectionsByMac(c, m, macConnectionMap, macConnectionsToDelete)

	err = m.Reboot()
	if err != nil {
		return fmt.Errorf("failed to reboot the machine: %v", err)
	}
	return nil
}

func flushInterfaceIpByMac(c cluster.TestCluster, m platform.Machine, macInterfaceMap map[string]string, macsList []string) {
	for _, mac := range macsList {
		InterfaceToFlush := macInterfaceMap[mac]
		c.MustSSH(m, fmt.Sprintf("sudo ip addr flush dev '%s'", InterfaceToFlush))
	}
}

func removeConnectionsByMac(c cluster.TestCluster, m platform.Machine, macConnectionMap map[string]string, macsList []string) {
	for _, mac := range macsList {
		connectionToDelete := macConnectionMap[mac]
		c.MustSSH(m, fmt.Sprintf("sudo nmcli con del '%s'", connectionToDelete))
	}
}

func checkExpectedInterfaces(c cluster.TestCluster, m platform.Machine, primaryMac, secondaryMac string) error {
	macConnectionMap, err := getMacConnectionMap(c, m)
	if err != nil {
		return fmt.Errorf("failed to get macConnectionMap: %v", err)
	}
	err = checkExpectedInterfacesStatus(c, m, macConnectionMap, []string{primaryMac, secondaryMac}, []string{})
	if err != nil {
		return fmt.Errorf("interfaces are not in expected status when connections do not exist: %v", err)
	}
	err = checkExpectedInterfacesIPAddress(c, m, macConnectionMap, []string{primaryMac}, []string{secondaryMac})
	if err != nil {
		return fmt.Errorf("interfaces are not in expected status when connections do not exist: %v", err)
	}
	return nil
}

func checkExpectedInterfacesStatus(c cluster.TestCluster, m platform.Machine, macConnectionMap map[string]string, expectedUpInterfacesMacList, expectedDownInterfacesMacList []string) error {
	failedConnections := []string{}
	for _, ifaceMac := range expectedUpInterfacesMacList {
		connectionName := macConnectionMap[ifaceMac]
		if !isConnectionUp(c, m, connectionName) {
			failedConnections = append(failedConnections, fmt.Sprintf("expected connection %s to be UP", connectionName))
		}
	}

	for _, ifaceMac := range expectedDownInterfacesMacList {
		connectionName := macConnectionMap[ifaceMac]
		if isConnectionUp(c, m, connectionName) {
			failedConnections = append(failedConnections, fmt.Sprintf("expected connection %s to be DOWN", connectionName))
		}
	}

	if len(failedConnections) != 0 {
		return fmt.Errorf(strings.Join(failedConnections, ","))
	}
	return nil
}

func isConnectionUp(c cluster.TestCluster, m platform.Machine, connectionName string) bool {
	return getConnectionStatus(c, m, connectionName) == "activated"
}

func getConnectionStatus(c cluster.TestCluster, m platform.Machine, connectionName string) string {
	return string(c.MustSSH(m, fmt.Sprintf("nmcli -f GENERAL.STATE con show '%s' | awk '{print $2}'", connectionName)))
}

func checkExpectedInterfacesIPAddress(c cluster.TestCluster, m platform.Machine, macConnectionMap map[string]string, expectedIpEnabledInterfaces, expectedIpDisabledInterfaces []string) error {
	failedConnections := []string{}
	for _, ifaceMac := range expectedIpEnabledInterfaces {
		connectionName := macConnectionMap[ifaceMac]
		if !isConnectionIpv4Enabled(c, m, connectionName) {
			failedConnections = append(failedConnections, fmt.Sprintf("expected connection %s to have an IPv4 Address", connectionName))
		}
	}

	for _, ifaceMac := range expectedIpDisabledInterfaces {
		connectionName := macConnectionMap[ifaceMac]
		if isConnectionIpv4Enabled(c, m, connectionName) {
			failedConnections = append(failedConnections, fmt.Sprintf("expected connection %s to not have an IPv4 Address", connectionName))
		}
	}

	if len(failedConnections) != 0 {
		return fmt.Errorf(strings.Join(failedConnections, ","))
	}
	return nil
}

func isConnectionIpv4Enabled(c cluster.TestCluster, m platform.Machine, connectionName string) bool {
	return getConnectionIpv4Addresses(c, m, connectionName) != ""
}

func getConnectionIpv4Addresses(c cluster.TestCluster, m platform.Machine, connectionName string) string {
	return string(c.MustSSH(m, fmt.Sprintf("nmcli -g IP4.ADDRESS con show '%s'", connectionName)))
}

func checkExpectedMACs(c cluster.TestCluster, m platform.Machine, expectedMacsList []string) {
	macConnectionMap, err := getMacConnectionMap(c, m)
	if err != nil {
		c.Fatalf(fmt.Sprintf("failed to get macConnectionMap: %v", err))
	}

	for _, expectedMac := range expectedMacsList {
		if _, exists := macConnectionMap[expectedMac]; !exists {
			c.Fatalf(fmt.Sprintf("expected Mac %s does not appear in macConnectionMap %v", expectedMac, macConnectionMap))
		}
	}
}

func getMacConnectionMap(c cluster.TestCluster, m platform.Machine) (map[string]string, error) {
	connectionNamesList := getConnectionsList(c, m)
	connectionDeviceMap, err := getConnectionDeviceMap(c, m, connectionNamesList)
	if err != nil {
		return nil, fmt.Errorf("failed to get connectionDeviceMap: %v", err)
	}

	macConnectionMap := map[string]string{}
	for _, connection := range connectionNamesList {
		interfaceMACAddress, err := getDeviceMAC(c, m, connectionDeviceMap[connection])
		if err != nil {
			return nil, fmt.Errorf("failed to fetch connection %s MAC Address: %v", connection, err)
		}
		macConnectionMap[interfaceMACAddress] = connection
	}
	return macConnectionMap, nil
}

func getMacInterfaceMap(c cluster.TestCluster, m platform.Machine) (map[string]string, error) {
	connectionNamesList := getConnectionsList(c, m)
	connectionDeviceMap, err := getConnectionDeviceMap(c, m, connectionNamesList)
	if err != nil {
		return nil, fmt.Errorf("failed to get connectionDeviceMap: %v", err)
	}

	macInterfaceMap := map[string]string{}
	for _, connection := range connectionNamesList {
		interfaceMACAddress, err := getDeviceMAC(c, m, connectionDeviceMap[connection])
		if err != nil {
			return nil, fmt.Errorf("failed to fetch connection %s MAC Address: %v", connection, err)
		}
		macInterfaceMap[interfaceMACAddress] = connectionDeviceMap[connection]
	}
	return macInterfaceMap, nil
}

func getConnectionsList(c cluster.TestCluster, m platform.Machine) []string {
	output := string(c.MustSSH(m, "nmcli -t -f NAME con show"))
	connectionNames := strings.Split(output, "\n")
	return connectionNames
}

func getConnectionDeviceMap(c cluster.TestCluster, m platform.Machine, connectionNamesList []string) (map[string]string, error) {
	connectionDeviceMap := map[string]string{}

	for _, connection := range connectionNamesList {
		deviceName := string(c.MustSSH(m, fmt.Sprintf("nmcli -g connection.interface-name con show '%s'", connection)))
		connectionDeviceMap[connection] = deviceName
	}
	return connectionDeviceMap, nil
}

func getDeviceMAC(c cluster.TestCluster, m platform.Machine, deviceName string) (string, error) {
	output := string(c.MustSSH(m, fmt.Sprintf("nmcli -g GENERAL.HWADDR device show '%s'", deviceName)))
	output = strings.Replace(output, "\\:", ":", -1)

	var macAddress net.HardwareAddr
	var err error
	if macAddress, err = net.ParseMAC(output); err != nil {
		return "", fmt.Errorf("failed to parse MAC address %v for device Name %s: %v", output, deviceName, err)
	}

	return macAddress.String(), nil
}
