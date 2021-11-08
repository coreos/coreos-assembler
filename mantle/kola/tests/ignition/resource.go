// Copyright 2017 CoreOS, Inc.
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

package ignition

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pin/tftp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/packet"
)

var (
	localClient = conf.Ignition(`{
		  "ignition": {
		      "version": "3.0.0"
		  },
		  "storage": {
		      "files": [
			  {
			      "path": "/var/resource/data",
			      "contents": {
				  "source": "data:,kola-data"
			      },
			      "mode": 420
			  },
			  {
			      "path": "/var/resource/http",
			      "contents": {
				  "source": "http://$IP/http"
			      },
			      "mode": 420
			  },
			  {
			      "path": "/var/resource/tftp",
			      "contents": {
				  "source": "tftp://$IP/tftp"
			      },
			      "mode": 420
			  }
		      ]
		  }
	      }`)
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.resource.local",
		Run:         resourceLocal,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"Serve": register.CreateNativeFuncWrap(Serve),
		},
		Tags: []string{"ignition"},
		// https://github.com/coreos/bugs/issues/2205
		ExcludePlatforms: []string{"do", "qemu-unpriv"},
		Timeout:          20 * time.Minute,
	})
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.resource.remote",
		Run:         resourceRemote,
		ClusterSize: 1,
		Flags:       []register.Flag{register.RequiresInternetAccess},
		Tags:        []string{"ignition"},
		// https://github.com/coreos/bugs/issues/2205 for DO
		ExcludePlatforms: []string{"do"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "3.0.0"
		  },
		  "storage": {
		      "files": [
			  {
			      "path": "/var/resource/http",
			      "contents": {
				  "source": "http://rh-kola-fixtures.s3.amazonaws.com/resources/anonymous"
			      },
			      "mode": 420
			  },
			  {
			      "path": "/var/resource/https",
			      "contents": {
				  "source": "https://rh-kola-fixtures.s3.amazonaws.com/resources/anonymous"
			      },
			      "mode": 420
			  },
			  {
			      "path": "/var/resource/s3-anon",
			      "contents": {
				  "source": "s3://rh-kola-fixtures/resources/anonymous"
			      },
			      "mode": 420
			  }
		      ]
		  }
	      }`),
	})
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.resource.s3",
		Run:         resourceS3,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		Tags:        []string{"ignition"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "3.0.0",
		      "config": {
		          "merge": [{
		              "source": "s3://rh-kola-fixtures/resources/authenticated-var-v3.ign"
		          }]
		      }
		  },
		  "storage": {
		      "files": [
			  {
			      "path": "/var/resource/s3-auth",
			      "contents": {
				  "source": "s3://rh-kola-fixtures/resources/authenticated"
			      },
			      "mode": 420
			  }
		      ]
		  }
	      }`),
	})
	// TODO: once Ignition supports this on all channels/distros
	//       this test should be rolled into coreos.ignition.resources.remote
	// Test specifically for versioned s3 objects
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.resource.s3.versioned",
		Run:         resourceS3Versioned,
		ClusterSize: 1,
		Flags:       []register.Flag{register.RequiresInternetAccess},
		Tags:        []string{"ignition"},
		// https://github.com/coreos/bugs/issues/2205 for DO
		ExcludePlatforms: []string{"do"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "3.0.0"
		  },
		  "storage": {
		      "files": [
			  {
			      "path": "/var/resource/original",
			      "contents": {
				  "source": "https://rh-kola-fixtures.s3.amazonaws.com/resources/versioned?versionId=Ym98GTx0npVaJznSAd0I1eUjFoZMP8Zo"
			      },
			      "mode": 420
			  },
			  {
			      "path": "/var/resource/latest",
			      "contents": {
				  "source": "https://rh-kola-fixtures.s3.amazonaws.com/resources/versioned"
			      },
			      "mode": 420
			  }
		      ]
		  }
	      }`),
		Distros: []string{"rhcos"},
	})
}

func resourceLocal(c cluster.TestCluster) {
	server := c.Machines()[0]

	c.RunCmdSyncf(server, "sudo systemd-run --quiet ./kolet run %s Serve", c.H.Name())

	ip := server.PrivateIP()
	if c.Platform() == packet.Platform {
		// private IP not configured in the initramfs
		ip = server.IP()
	}

	var conf *conf.UserData = localClient
	client, err := c.NewMachine(conf.Subst("$IP", ip))
	if err != nil {
		c.Fatalf("starting client: %v", err)
	}

	checkResources(c, client, map[string]string{
		"data": "kola-data",
		"http": "kola-http",
		"tftp": "kola-tftp",
	})
}

func resourceRemote(c cluster.TestCluster) {
	m := c.Machines()[0]

	checkResources(c, m, map[string]string{
		"http":    "kola-anonymous",
		"https":   "kola-anonymous",
		"s3-anon": "kola-anonymous",
	})
}

func resourceS3(c cluster.TestCluster) {
	m := c.Machines()[0]

	checkResources(c, m, map[string]string{
		// object accessible by any authenticated S3 user, such as
		// the IAM role associated with the instance
		"s3-auth": "kola-authenticated",
		// object created by configuration accessible by any authenticated
		// S3 user, such as the IAM role associated with the instance
		"s3-config": "kola-config",
	})

	// verify that the objects are inaccessible anonymously
	for _, objectName := range []string{"authenticated", "authenticated.ign"} {
		_, _, err := m.SSH("curl -sf https://rh-kola-fixtures.s3.amazonaws.com/resources/" + objectName)
		if err == nil {
			c.Fatal("anonymously fetching authenticated resource should have failed, but did not")
		}
	}

	// ...but that the anonymous object is accessible
	c.RunCmdSync(m, "curl -sf https://rh-kola-fixtures.s3.amazonaws.com/resources/anonymous")
}

func resourceS3Versioned(c cluster.TestCluster) {
	m := c.Machines()[0]

	checkResources(c, m, map[string]string{
		"original": "original",
		"latest":   "updated",
	})
}

func checkResources(c cluster.TestCluster, m platform.Machine, resources map[string]string) {
	for filename, expectedContents := range resources {
		contents := c.MustSSH(m, fmt.Sprintf("sudo cat /var/resource/%s", filename))
		if string(contents) != expectedContents {
			c.Fatalf("%s: %q != %q", filename, expectedContents, contents)
		}
	}
}

func Serve() error {
	go func() {
		http.HandleFunc("/http", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "text/plain")
			_, _ = w.Write([]byte("kola-http"))
		})
		err := http.ListenAndServe(":80", nil)
		fmt.Println(err)
	}()

	go func() {
		readHandler := func(filename string, r io.ReaderFrom) error {
			switch filename {
			case "/tftp":
				_, _ = r.ReadFrom(bytes.NewBufferString("kola-tftp"))
			default:
				return fmt.Errorf("404 not found")
			}
			return nil
		}
		server := tftp.NewServer(readHandler, nil)
		err := server.ListenAndServe(":69")
		fmt.Println(err)
	}()

	select {}
}
