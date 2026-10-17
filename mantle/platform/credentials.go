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
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh/agent"
)

// SystemdCredentialVirtiofsTag is the well-known virtiofs tag used to pass
// systemd credentials into a VM. The guest initramfs imports files from this
// share into /run/credentials/@initrd/. See also:
// https://github.com/coreos/fedora-coreos-config/pull/4230
const SystemdCredentialVirtiofsTag = "io.systemd.credentials"

const systemdTmpfilesExtraCredential = "tmpfiles.extra"

// SystemdSSHtmpfilesExtra builds the tmpfiles.extra systemd credential content
// that provisions SSH authorized_keys for the given user.
// See https://systemd.io/CREDENTIALS/
func SystemdSSHtmpfilesExtra(user string, keys []*agent.Key) (string, error) {
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

	return fmt.Sprintf("d %s/.ssh %s %s %s -\nf~ %s/.ssh/authorized_keys 0600 %s %s - %s",
		homeDir, sshDirMode, user, user,
		homeDir, user, user,
		keysB64), nil
}

// WriteSystemdSSHCredentialsDir writes tmpfiles.extra into dir for import
// via a virtiofs share tagged SystemdCredentialVirtiofsTag.
func WriteSystemdSSHCredentialsDir(dir, user string, keys []*agent.Key) error {
	content, err := SystemdSSHtmpfilesExtra(user, keys)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, systemdTmpfilesExtraCredential)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func sshHomeDir(user string) string {
	if user == "root" {
		return "/root"
	}
	return fmt.Sprintf("/var/home/%s", user)
}
