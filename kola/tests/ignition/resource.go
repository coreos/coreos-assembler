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

	"github.com/pin/tftp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

var (
	localClient = conf.Ignition(`{
		  "ignition": {
		      "version": "2.1.0"
		  },
		  "storage": {
		      "files": [
			  {
			      "filesystem": "root",
			      "path": "/resource/data",
			      "contents": {
				  "source": "data:,kola-data"
			      }
			  },
			  {
			      "filesystem": "root",
			      "path": "/resource/http",
			      "contents": {
				  "source": "http://$IP/http"
			      }
			  },
			  {
			      "filesystem": "root",
			      "path": "/resource/tftp",
			      "contents": {
				  "source": "tftp://$IP/tftp"
			      }
			  }
		      ]
		  }
	      }`)
)

func init() {
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_1.resource.local",
		Run:         resourceLocal,
		ClusterSize: 1,
		NativeFuncs: map[string]func() error{
			"Serve": Serve,
		},
	})
	register.Register(&register.Test{
		Name:             "coreos.ignition.v2_1.resource.remote",
		Run:              resourceRemote,
		ClusterSize:      1,
		ExcludePlatforms: []string{"qemu"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "2.1.0"
		  },
		  "storage": {
		      "files": [
			  {
			      "filesystem": "root",
			      "path": "/resource/http",
			      "contents": {
				  "source": "http://s3-us-west-2.amazonaws.com/kola-fixtures/resources/anonymous"
			      }
			  },
			  {
			      "filesystem": "root",
			      "path": "/resource/https",
			      "contents": {
				  "source": "https://s3-us-west-2.amazonaws.com/kola-fixtures/resources/anonymous"
			      }
			  },
			  {
			      "filesystem": "root",
			      "path": "/resource/s3-anon",
			      "contents": {
				  "source": "s3://kola-fixtures/resources/anonymous"
			      }
			  }
		      ]
		  }
	      }`),
	})
	register.Register(&register.Test{
		Name:        "coreos.ignition.v2_1.resource.s3",
		Run:         resourceS3,
		ClusterSize: 1,
		Platforms:   []string{"aws"},
		UserData: conf.Ignition(`{
		  "ignition": {
		      "version": "2.1.0",
		      "config": {
		          "append": [{
		              "source": "s3://kola-fixtures/resources/authenticated.ign"
		          }]
		      }
		  },
		  "storage": {
		      "files": [
			  {
			      "filesystem": "root",
			      "path": "/resource/s3-auth",
			      "contents": {
				  "source": "s3://kola-fixtures/resources/authenticated"
			      }
			  }
		      ]
		  }
	      }`),
	})
}

func resourceLocal(c cluster.TestCluster) {
	server := c.Machines()[0]

	c.MustSSH(server, fmt.Sprintf("sudo systemd-run --quiet ./kolet run %s Serve", c.Name()))

	client, err := c.NewMachine(localClient.Subst("$IP", server.PrivateIP()))
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
		_, _, err := m.SSH("curl -sf https://s3-us-west-2.amazonaws.com/kola-fixtures/resources/" + objectName)
		if err == nil {
			c.Fatal("anonymously fetching authenticated resource should have failed, but did not")
		}
	}

	// ...but that the anonymous object is accessible
	c.MustSSH(m, "curl -sf https://s3-us-west-2.amazonaws.com/kola-fixtures/resources/anonymous")
}

func checkResources(c cluster.TestCluster, m platform.Machine, resources map[string]string) {
	for filename, expectedContents := range resources {
		contents := c.MustSSH(m, fmt.Sprintf("sudo cat /resource/%s", filename))
		if string(contents) != expectedContents {
			c.Fatalf("%s: %q != %q", filename, expectedContents, contents)
		}
	}
}

func Serve() error {
	go func() {
		http.HandleFunc("/http", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "text/plain")
			w.Write([]byte("kola-http"))
		})
		err := http.ListenAndServe(":80", nil)
		fmt.Println(err)
	}()

	go func() {
		readHandler := func(filename string, r io.ReaderFrom) error {
			switch filename {
			case "/tftp":
				r.ReadFrom(bytes.NewBufferString("kola-tftp"))
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
