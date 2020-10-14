// Copyright 2020 Red Hat
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
	"fmt"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/vincent-petithory/dataurl"
)

var baseurl = []byte(`[cincinnati]
base_url = "http://127.0.0.1:9001"`)

var updates = []byte(`updates.enabled = true`)

var graph = []byte(`{
	"nodes": [
	  {
		"version": "${VERSION}",
		"metadata": {
		  "org.fedoraproject.coreos.scheme": "checksum",
		  "org.fedoraproject.coreos.releases.age_index": "0",
		  "org.fedoraproject.coreos.updates.deadend_reason": "https://github.com/coreos/fedora-coreos-tracker/issues/215",
		  "org.fedoraproject.coreos.updates.deadend": "true"
		},
		"payload": "${PAYLOAD}"
	  }
	],
	"edges": []
  }`)

var script = []byte(`#!/bin/bash
  VERSION="$(/usr/bin/rpm-ostree status --json | jq '.deployments[0].version' -r)"
  PAYLOAD="$(/usr/bin/rpm-ostree status --json | jq '.deployments[0].checksum' -r)"
  sed -i 's/\"version\":.*/\"version\": \"'$VERSION'\",/g' /var/www/html/v1/graph
  sed -i 's/\"payload\":.*/\"payload\": \"'$PAYLOAD'\"/g' /var/www/html/v1/graph`)

var zincatidropin = []byte(`[Service]
Environment=ZINCATI_VERBOSITY="-vvvv"`)

var nginxdropin = []byte(`[Unit]
Description=nginx Server
After=network-online.target
Requires=network-online.target
Before=zincati.service
[Service]
Type=oneshot
Restart=on-failure
ExecStartPre=-/usr/bin/podman pull docker.io/nginx:latest
ExecStartPre=/usr/local/bin/extract-dead-end-info.sh
ExecStart=/usr/bin/podman run --name nginx \
					  -p 9001:80 \
					  -v /var/www/html:/usr/share/nginx/html:z \
					  nginx
ExecStart=/bin/bash -c 'while [[ $(curl -s -o /dev/null -I -w "%{http_code}" http://127.0.0.1:9001/v1/graph) -ne "200" ]]; do sleep 5; done'
ExecStop=-/usr/bin/podman stop nginx
RemainAfterExit=yes
[Install]
WantedBy=multi-user.target`)

// Related to https://github.com/coreos/zincati/issues/90
// This test will verify if zincati exposes the dead-end
// release information by writing an MOTD file in `/run/motd.d`.
func init() {
	register.RegisterTest(&register.Test{
		Name:        "fcos.zincati.deadend.motd-info",
		Run:         checkDeadEndReleaseInformation,
		ClusterSize: 1,
		Tags:        []string{"fcos", "zincati"},
		UserDataV3: conf.Ignition(fmt.Sprintf(`{
    "ignition": {"version": "3.0.0"},
	"storage": {
		"files": [
		  {
			"path": "/etc/zincati/config.d/99-cincinnati-baseurl.toml",
			"contents": {
			  "source": %q
			}
		  },
		  {
			"path": "/etc/zincati/config.d/99-enable-updates.toml",
			"contents": {
			  "source": %q
			}
		  },
		  {
			"path": "/var/www/html/v1/graph",
			"contents": {
			  "source": %q
			}
		  },
		  {
			"path": "/usr/local/bin/extract-dead-end-info.sh",
			"contents": {
			  "source": %q
			},
			"mode": 493
		  }
		]
	  },
	"systemd": {
		"units": [
		  {
			"dropins": [
			  {
				"contents": %q,
				"name": "verbose.conf"
			  }
			],
			"name": "zincati.service"
		  },
		  {
			"contents": %q,
			"enabled": true,
			"name": "nginx.service"
		  }
		]
	  }
	}`, dataurl.EncodeBytes(baseurl), dataurl.EncodeBytes(updates), dataurl.EncodeBytes(graph), dataurl.EncodeBytes(script), dataurl.EncodeBytes(zincatidropin), dataurl.EncodeBytes(nginxdropin))),
		ExcludeDistros: []string{"rhcos"},
	})
}

func checkDeadEndReleaseInformation(c cluster.TestCluster) {
	m := c.Machines()[0]
	// MustSSH function will throw an error if the exit code
	// of the command is anything other than 0.
	c.MustSSH(m, "test -f /run/motd.d/85-zincati-deadend.motd")
}
