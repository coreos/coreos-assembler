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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func testSSHKeys(t *testing.T) []*agent.Key {
	t.Helper()
	signer, err := ssh.NewSignerFromKey(mustGenerateTestKey(t))
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey()
	return []*agent.Key{{
		Format:  pub.Type(),
		Blob:    pub.Marshal(),
		Comment: "test@example.com",
	}}
}

func mustGenerateTestKey(t *testing.T) any {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSystemdSSHtmpfilesExtra(t *testing.T) {
	keys := testSSHKeys(t)

	content, err := SystemdSSHtmpfilesExtra("root", keys)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "d /root/.ssh 0750 root root -") {
		t.Errorf("missing .ssh directory line: %q", content)
	}
	if !strings.Contains(content, "f~ /root/.ssh/authorized_keys 0600 root root - ") {
		t.Errorf("missing authorized_keys line: %q", content)
	}

	parts := strings.Split(content, " - ")
	if len(parts) < 2 {
		t.Fatalf("expected base64 keys suffix in tmpfiles content: %q", content)
	}
	keysB64 := strings.TrimSpace(parts[len(parts)-1])
	decoded, err := base64.StdEncoding.DecodeString(keysB64)
	if err != nil {
		t.Fatalf("decoding keys: %v", err)
	}
	if !strings.Contains(string(decoded), "test@example.com") {
		t.Errorf("decoded keys missing comment: %q", decoded)
	}
}

func TestWriteSystemdSSHCredentialsDir(t *testing.T) {
	dir := t.TempDir()
	keys := testSSHKeys(t)

	if err := WriteSystemdSSHCredentialsDir(dir, "root", keys); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, systemdTmpfilesExtraCredential)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/root/.ssh/authorized_keys") {
		t.Errorf("unexpected credential content: %q", data)
	}
}

func TestSystemdSSHtmpfilesExtraErrors(t *testing.T) {
	_, err := SystemdSSHtmpfilesExtra("", nil)
	if err == nil {
		t.Fatal("expected error for empty user")
	}
	_, err = SystemdSSHtmpfilesExtra("root", nil)
	if err == nil {
		t.Fatal("expected error for empty keys")
	}
}
