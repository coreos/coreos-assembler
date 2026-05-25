// Copyright 2026 Red Hat
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

package platform

import (
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh/agent"
)

const systemdCredentialPrefix = "io.systemd.credential.binary:"

// SystemdSMBIOSSSHCredential builds a QEMU -smbios type=11 value that provisions
// SSH authorized_keys via the systemd tmpfiles.extra system credential.
// See https://systemd.io/CREDENTIALS/
func SystemdSMBIOSSSHCredential(user string, keys []*agent.Key) (string, error) {
	if user == "" {
		return "", fmt.Errorf("SSH user must be set")
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("no SSH keys provided")
	}

	var keyLines []string
	for _, key := range keys {
		keyLines = append(keyLines, key.String())
	}
	keysContent := strings.Join(keyLines, "\n") + "\n"
	keysB64 := base64.StdEncoding.EncodeToString([]byte(keysContent))

	homeDir := sshHomeDir(user)
	sshDirMode := "0700"
	if user == "root" {
		sshDirMode = "0750"
	}

	tmpfiles := fmt.Sprintf("d %s/.ssh %s %s %s -\nf~ %s/.ssh/authorized_keys 0600 %s %s - %s",
		homeDir, sshDirMode, user, user,
		homeDir, user, user,
		keysB64)

	tmpfilesB64 := base64.StdEncoding.EncodeToString([]byte(tmpfiles))
	return fmt.Sprintf("type=11,value=%stmpfiles.extra=%s", systemdCredentialPrefix, tmpfilesB64), nil
}

func sshHomeDir(user string) string {
	if user == "root" {
		return "/root"
	}
	return fmt.Sprintf("/var/home/%s", user)
}
