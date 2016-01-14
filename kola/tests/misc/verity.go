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
	"bytes"
	"fmt"

	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         VerityVerify,
		ClusterSize: 1,
		Name:        "coreos.verity.verify",
		Platforms:   []string{"qemu", "aws", "gce"},
	})
}

// VerityVerify asserts that the filesystem mounted on /usr matches the
// dm-verity hash that is embedded in the CoreOS kernel.
func VerityVerify(c platform.TestCluster) error {
	m := c.Machines()[0]

	// extract verity hash from kernel
	hash, err := m.SSH("dd if=/boot/coreos/vmlinuz-a skip=64 count=64 bs=1 2>/dev/null")
	if err != nil {
		return fmt.Errorf("failed to extract verity hash from kernel: %v: %v", hash, err)
	}

	// find /usr dev
	usrdev, err := m.SSH("findmnt -no SOURCE /usr")
	if err != nil {
		return fmt.Errorf("failed to find device for /usr: %v: %v", usrdev, err)
	}

	// figure out partition size for hash dev offset
	offset, err := m.SSH("sudo e2size " + string(usrdev))
	if err != nil {
		return fmt.Errorf("failed to find /usr partition size: %v: %v", offset, err)
	}

	offset = bytes.TrimSpace(offset)
	veritycmd := fmt.Sprintf("sudo veritysetup verify --verbose --hash-offset=%s %s %s %s", offset, usrdev, usrdev, hash)

	verify, err := m.SSH(veritycmd)
	if err != nil {
		return fmt.Errorf("verity hash verification on %s failed: %v: %v", usrdev, verify, err)
	}

	return nil
}
