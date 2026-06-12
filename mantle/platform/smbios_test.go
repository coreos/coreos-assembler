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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func TestSystemdSMBIOSSSHCredential(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey()
	keys := []*agent.Key{{Format: pub.Type(), Blob: pub.Marshal()}}

	val, err := SystemdSMBIOSSSHCredential("root", keys)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(val, "type=11,value=io.systemd.credential.binary:tmpfiles.extra=") {
		t.Fatalf("unexpected smbios value prefix: %q", val)
	}

	payloadB64 := strings.TrimPrefix(val, "type=11,value=io.systemd.credential.binary:tmpfiles.extra=")
	tmpfiles, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tmpfiles), "/root/.ssh") {
		t.Fatalf("tmpfiles missing root ssh dir: %q", tmpfiles)
	}
	if !strings.Contains(string(tmpfiles), "authorized_keys") {
		t.Fatalf("tmpfiles missing authorized_keys: %q", tmpfiles)
	}
}

func TestSystemdSMBIOSSSHCredentialNoKeys(t *testing.T) {
	_, err := SystemdSMBIOSSSHCredential("root", nil)
	if err == nil {
		t.Fatal("expected error for empty keys")
	}
}
