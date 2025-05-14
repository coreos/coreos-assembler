// Copyright 2020 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package util

import (
	"regexp"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

// TangServer contains fields required to set up a tang server
// Note: Placing it here to avoid circular dependency issue
type TangServer struct {
	Machine    platform.Machine
	Address    string
	Thumbprint string
}

func mustMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatalf("Failed to match regexp %s: %v", r, err)
	}
	if !m {
		c.Fatalf("Regexp %s did not match text: %s", r, output)
	}
}

func mustNotMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatalf("Failed to match regexp %s: %v", r, err)
	}
	if m {
		c.Fatalf("Regexp %s matched text: %s", r, output)
	}
}

// LUKSSanityTest verifies that the rootfs is encrypted with LUKS
func LUKSSanityTest(c cluster.TestCluster, tangd TangServer, m platform.Machine, tpm2, killTangAfterFirstBoot bool, rootPart string) {
	luksDump := c.MustSSH(m, "sudo cryptsetup luksDump "+rootPart)
	// Yes, some hacky regexps.  There is luksDump --debug-json but we'd have to massage the JSON
	// out of other debug output and it's not clear to me it's going to be more stable.
	// We're just going for a basic sanity check here.
	mustMatch(c, "Cipher: *aes", luksDump)
	mustNotMatch(c, "Cipher: *cipher_null-ecb", luksDump)
	mustMatch(c, "0: *clevis", luksDump)
	mustNotMatch(c, "9: *coreos", luksDump)

	s := c.MustSSH(m, "sudo clevis luks list -d "+rootPart)
	if tpm2 {
		mustMatch(c, "tpm2", s)
	}
	tangConf := TangServer{}
	if tangd != tangConf {
		mustMatch(c, "tang", s)
		// And validate we can automatically unlock it on reboot.
		// We kill the tang server if we're testing thresholding
		if killTangAfterFirstBoot {
			tangd.Machine.Destroy()
		}
	}
	err := m.Reboot()
	if err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	luksDump = c.MustSSH(m, "sudo cryptsetup luksDump "+rootPart)
	mustMatch(c, "Cipher: *aes", luksDump)
}
