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

package packages

import (
	"bytes"
	"fmt"

	"github.com/coreos/mantle/kola/cluster"
)

func ipvsadm(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Test it runs at all
	out, err := c.SSH(m, "sudo ipvsadm")
	if err != nil {
		c.Fatalf("could not run ipvsadm: %v", err)
	}
	if !bytes.Contains(out, []byte(`IP Virtual Server version`)) {
		c.Fatalf("unexpected ipvsadm output: %v", string(out))
	}

	// Test by using the example from the man page
	cmd := `echo " 
	-A -t 207.175.44.110:80 -s rr
	-a -t 207.175.44.110:80 -r 192.168.10.1:80 -m
	-a -t 207.175.44.110:80 -r 192.168.10.2:80 -m
	-a -t 207.175.44.110:80 -r 192.168.10.3:80 -m
	-a -t 207.175.44.110:80 -r 192.168.10.4:80 -m
	-a -t 207.175.44.110:80 -r 192.168.10.5:80 -m
	" | sudo ipvsadm -R`
	out, err = c.SSH(m, cmd)
	if err != nil {
		c.Fatalf("could not run ipvsadm: %v", err)
	}

	// Test we can read back what we just did
	out, err = c.SSH(m, "sudo ipvsadm -Ln")
	if err != nil {
		c.Fatalf("could not run ipvsadm: %v", err)
	}
	if !bytes.Contains(out, []byte(`TCP  207.175.44.110:80 rr`)) {
		c.Fatalf("could not create virtual service %v", string(out))
	}
	for i := 1; i <= 5; i++ {
		ip := []byte(fmt.Sprintf("-> 192.168.10.%d:80", i))
		if !bytes.Contains(out, ip) {
			c.Fatalf("did not add real service %v", string(ip))
		}
	}

	// Test we can delete the service
	out, err = c.SSH(m, "sudo ipvsadm -D -t 207.175.44.110:80")
	if err != nil {
		c.Fatalf("could not run ipvsadm: %v", err)
	}

	// Ensure it was really deleted
	out, err = c.SSH(m, "sudo ipvsadm -Ln")
	if err != nil {
		c.Fatalf("could not run ipvsadm: %v", err)
	}
	if bytes.Contains(out, []byte(`TCP 207.175.44.110:80 rr`)) {
		c.Fatalf("could not delete virtual service")
	}
}
