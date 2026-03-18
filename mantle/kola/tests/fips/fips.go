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
		Description: "Verify that fips enabled works.",
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
		Description: "Verify that fips enabled works if custom partitions are present.",
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos"},
		Platforms:   []string{"qemu"},
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
	// Test that using TLS works in FIPS mode by having Ignition fetch
	// a remote resource over HTTPS with FIPS compatible algorithms.
	// See https://issues.redhat.com/browse/COS-3487
	// Note that 34.172.244.189, running RHCOS 9.6 (build 20260312-0) on
	// Google Cloud Platform, provides HTTPS services using nginx-126:10.1.
	register.RegisterTest(&register.Test{
		Run:                  fipsEnableTestTLS,
		ClusterSize:          1,
		Name:                 `fips.enable.tls`,
		Description:          "Verify that fips enabled works if fetching a remote resource over HTTPS with FIPS compatible algorithms.",
		Flags:                []register.Flag{},
		Tags:                 []string{kola.NeedsInternetTag},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"}, // only test on x86_64
		UserData: conf.Ignition(`{
			"ignition": {
				"config": {
					"replace": {
						"source": null,
						"verification": {}
					}
				},
				"security": {
					"tls": {
						"certificateAuthorities": [
							{
								"compression": "gzip",
								"source": "data:;base64,H4sIAAAAAAAC/2SUTdN7yhbF5z7FnaduIUEY/AfdNFoQ2luYeUkkSLwk0fj0t57nnsk5Zw9/u2rV3lVrrf/+DEQGdv+jIhJiHasgRL+UcTBGVqOqoJdqQDEENY4q7G3jjqz64bkgmSjY2p8qKY3Gm0w1P7VOfYbvc+kCH9kM9AE1N1Q58G0APkKA0oi0nR+tMAp1p46NeKtUGGYXa59dMDXvpes0KXU2tHc0Z2PcBgjJD9x+4fYDf1kDbg7hqEFTLfZ9TYPECGIYYp04P+LZxeWw7nZM+cq68ueFHP/zPAh9oNW14QFNVUHaq3VtQHBWT5t93FhwWUnrFkyamb0KudwI1bgZ+jV86+TQaPrgrd/RvQRx+Xqx6lfffdnAi9FQTRw82PZ8HHM7Gt6IeaTV10vepwcr0iU/QLV250dMAquRsMi/rIJevXRV71auDPfnyVnHiD9UX4ukfUm5cGJkdr29FkGZ/DU58SQxrnjd9p/yAu+cgNL7JXq3j0oxd16NyiU3z+5bYyH5HK6NhTg5Y9J7labtJrhbee+zXBdD+7LGb42PLOOCbqoyfBp/93jvZBIBj6gGXwvCEsjE6q5o2iSGNe/EjLESWAdPw71i2EmRnCO22Y1k0i41HjGZaQLzaiM8r4kbDH3xewJdNBjhfh0ZGpTmt67ZfuLOYdjxwPJt4dh9RN/3Ba9LV78JzkjK0+rrV97xHamGieTUaRcqNVNxYdgPp5tRKtgt0PSg+wglmZ7obbN+cfNuR1JkJPLMJSx2tO1Pb+jtxnCJCpR3W8EOiGWaC8LlXZ9nT0nEJyeJm2iwTzRW/Km6Yrn189YdH5Y3Fy2K1OJT+VgDPoA939NEA2cG1m5s+jIENxlB4KgwB1SvUy0mXAd8k4WAUrVO8YmmEPqRCSiiGv3dEwhqysC+RHjcuDkE1f/FBKTXfpSRh97WU19wD/tV3W6NMRK2z0HSvf/mWOYnUX9ZFgI7MvEKKldoWtHGxSRxfLc9bbQrzSORUfLm2t2J3Bw42QVnTblbl4ww4DnmpaGN6OOuxKL79dTj7LqlPtcfH3vqK2XBC93VVzwavjTN+hnskvauGroj3Z4z442peDVjPXB4urDVFX8qsv924b7x7fuXaytdrbqyy53RL0r5my2udheyLWPJcHh6t5yZi5zjnEAxTRgeh+oRDpwkhnOunL/pQRjchO3LaPwaSuLkDs/HhqodOno6JA9BZsPYZ9BgdPPgKB9l1z/H9X3L8uU11Hv/6gSTZXpsGxm4Ts/24SbbT1pehxgCjh7XYY1Dc5KYqyh5tBYH/bk/vtbDHLgW/+yt83gTr1McTwczY08UWFIvfchVldSHYixka6XAN4lyEZinOkjjINfRNRpa3iuUs77t94NcgU8U7j6FtV0TCWTp7Qym5jn1jvupgkCKYuqEyhlhpig1gy0fs7mEuCi8ERNxt1R5+hHP5SZPX1AH1evyXCuzfZeP5uRVbNLbXbZ8UBvIw5tZtt1wqIKDRXA81H/+ML9ljVzt3wX+vwAAAP//bBEEnd0FAAA="
							}
						]
					}
				},
				"timeouts": {},
				"version": "3.4.0"
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
					},
					{
						"path": "/var/resource/https-fips",
						"contents": {
							"source": "https://34.172.244.189:8443/index.html"
						}
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

func fipsEnableTestTLS(c cluster.TestCluster) {
	fipsEnableTest(c)
	m := c.Machines()[0]
	c.AssertCmdOutputContains(m, `cat /var/resource/https-fips`, "This file was served from an RHCOS FIPS-hardened server.")
}
