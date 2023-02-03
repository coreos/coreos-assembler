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

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/packet"
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
		Description: "Verify that we can fetch Ignition files through local, http and tftp.",
		Run:         resourceLocal,
		ClusterSize: 1,
		NativeFuncs: map[string]register.NativeFuncWrap{
			"Serve": register.CreateNativeFuncWrap(Serve),
		},
		Tags:             []string{"ignition"},
		ExcludePlatforms: []string{"qemu"},
		Timeout:          20 * time.Minute,
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
