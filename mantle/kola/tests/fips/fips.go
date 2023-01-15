package fips

import (
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

func init() {
	// Minimal test case to test FIPS enabling at first boot
	register.RegisterTest(&register.Test{
		Run:         fipsEnableTest,
		ClusterSize: 1,
		Name:        `fips.enable`,
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos"},
		UserData: conf.Ignition(`{
			"ignition": {
				"config": {
					"replace": {
						"source": null,
						"verification": {}
					}
				},
				"security": {
					"tls": {}
				},
				"timeouts": {},
				"version": "3.0.0"
			},
			"passwd": {},
			"storage": {
				"files": [
					{
						"group": {
							"name": "root"
						},
						"overwrite": true,
						"path": "/etc/ignition-machine-config-encapsulated.json",
						"user": {
							"name": "root"
						},
						"contents": {
							"source": "data:,%7B%22metadata%22%3A%7B%22name%22%3A%22rendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22selfLink%22%3A%22%2Fapis%2Fmachineconfiguration.openshift.io%2Fv1%2Fmachineconfigs%2Frendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22uid%22%3A%2248871c03-899d-4332-a5f5-bef94e54b23f%22%2C%22resourceVersion%22%3A%224168%22%2C%22generation%22%3A1%2C%22creationTimestamp%22%3A%222019-11-04T15%3A54%3A08Z%22%2C%22annotations%22%3A%7B%22machineconfiguration.openshift.io%2Fgenerated-by-controller-version%22%3A%22bd846958bc95d049547164046a962054fca093df%22%7D%2C%22ownerReferences%22%3A%5B%7B%22apiVersion%22%3A%22machineconfiguration.openshift.io%2Fv1%22%2C%22kind%22%3A%22MachineConfigPool%22%2C%22name%22%3A%22worker%22%2C%22uid%22%3A%223d0dee9e-c9d6-4656-a4a9-81785b9ab01a%22%2C%22controller%22%3Atrue%2C%22blockOwnerDeletion%22%3Atrue%7D%5D%7D%2C%22spec%22%3A%7B%22osImageURL%22%3A%22registry.svc.ci.openshift.org%2Focp%2F4.3-2019-11-04-125204%40sha256%3A8a344c5b157bd01c3ca1abfcef0004fc39f5d69cac1cdaad0fd8dd332ad8e272%22%2C%22config%22%3A%7B%22ignition%22%3A%7B%22config%22%3A%7B%7D%2C%22security%22%3A%7B%22tls%22%3A%7B%7D%7D%2C%22timeouts%22%3A%7B%7D%2C%22version%22%3A%223.0.0%22%7D%2C%22networkd%22%3A%7B%7D%2C%22passwd%22%3A%7B%7D%2C%22storage%22%3A%7B%7D%2C%22systemd%22%3A%7B%7D%7D%2C%22kernelArguments%22%3A%5B%5D%2C%22fips%22%3Atrue%7D%7D",
							"verification": {}
						},
						"mode": 420
					}
				]
			}
		}`),
	})
	// We currently extract the FIPS config from an encapsulated Ignition
	// config provided by the Machine Config Operator. We test here that this
	// logic still works if custom partitions are present. This will no longer
	// be needed once Ignition understands FIPS directly.
	// This only works on QEMU as the device name (vda) is hardcoded.
	register.RegisterTest(&register.Test{
		Run:         fipsEnableTest,
		ClusterSize: 1,
		Name:        `fips.enable.partitions`,
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu", "qemu-unpriv"},
		UserData: conf.Ignition(`{
			"ignition": {
				"config": {
					"replace": {
						"source": null,
						"verification": {}
					}
				},
				"security": {
					"tls": {}
				},
				"timeouts": {},
				"version": "3.0.0"
			},
			"passwd": {},
			"storage": {
				"disks": [
					{
						"device": "/dev/vda",
						"partitions": [
							{
								"label": "CONTR",
								"sizeMiB": 0,
								"startMiB": 0
							}
						]
					}
				],
				"files": [
					{
						"group": {
							"name": "root"
						},
						"overwrite": true,
						"path": "/etc/ignition-machine-config-encapsulated.json",
						"user": {
							"name": "root"
						},
						"contents": {
							"source": "data:,%7B%22metadata%22%3A%7B%22name%22%3A%22rendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22selfLink%22%3A%22%2Fapis%2Fmachineconfiguration.openshift.io%2Fv1%2Fmachineconfigs%2Frendered-worker-1cc576110e0cf8396831ce4016f63900%22%2C%22uid%22%3A%2248871c03-899d-4332-a5f5-bef94e54b23f%22%2C%22resourceVersion%22%3A%224168%22%2C%22generation%22%3A1%2C%22creationTimestamp%22%3A%222019-11-04T15%3A54%3A08Z%22%2C%22annotations%22%3A%7B%22machineconfiguration.openshift.io%2Fgenerated-by-controller-version%22%3A%22bd846958bc95d049547164046a962054fca093df%22%7D%2C%22ownerReferences%22%3A%5B%7B%22apiVersion%22%3A%22machineconfiguration.openshift.io%2Fv1%22%2C%22kind%22%3A%22MachineConfigPool%22%2C%22name%22%3A%22worker%22%2C%22uid%22%3A%223d0dee9e-c9d6-4656-a4a9-81785b9ab01a%22%2C%22controller%22%3Atrue%2C%22blockOwnerDeletion%22%3Atrue%7D%5D%7D%2C%22spec%22%3A%7B%22osImageURL%22%3A%22registry.svc.ci.openshift.org%2Focp%2F4.3-2019-11-04-125204%40sha256%3A8a344c5b157bd01c3ca1abfcef0004fc39f5d69cac1cdaad0fd8dd332ad8e272%22%2C%22config%22%3A%7B%22ignition%22%3A%7B%22config%22%3A%7B%7D%2C%22security%22%3A%7B%22tls%22%3A%7B%7D%7D%2C%22timeouts%22%3A%7B%7D%2C%22version%22%3A%223.0.0%22%7D%2C%22networkd%22%3A%7B%7D%2C%22passwd%22%3A%7B%7D%2C%22storage%22%3A%7B%7D%2C%22systemd%22%3A%7B%7D%7D%2C%22kernelArguments%22%3A%5B%5D%2C%22fips%22%3Atrue%7D%7D",
							"verification": {}
						},
						"mode": 420
					}
				],
				"filesystems": [
					{
						"device": "/dev/disk/by-partlabel/CONTR",
						"format": "xfs",
						"path": "/var/lib/containers",
						"wipeFilesystem": true
					}
				]
			},
			"systemd": {
				"units": [
					{
						"contents": "[Mount]\nWhat=/dev/disk/by-partlabel/CONTR\nWhere=/var/lib/containers\nType=xfs\nOptions=defaults\n[Install]\nWantedBy=local-fs.target",
						"enabled": true,
						"name": "var-lib-containers.mount"
					}
				]
			}
		}`),
	})
}

// Test: Run basic FIPS test
func fipsEnableTest(c cluster.TestCluster) {
	m := c.Machines()[0]
	c.AssertCmdOutputContains(m, `cat /proc/sys/crypto/fips_enabled`, "1")
	c.AssertCmdOutputContains(m, `update-crypto-policies --show`, "FIPS")
}
