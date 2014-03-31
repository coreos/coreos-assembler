package coretest

import (
	"encoding/hex"
	"fmt"
	"crypto/rand"
	"strings"
	"testing"
)

const (
	targetAddress = "http://127.0.0.1:4001/v2/keys/"
	helloStr      = "Hello world"
	newHelloStr   = "Hello etcd"
	keyNotFound   = "Key not found"
)

var (
	// retry is used to avoid getting server error when leader election
	retry = []string{"--retry", "5", "--retry-delay", "2", "--silent"}
)

// generateKey generate's a 16 byte random string.
func generateKey() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}

	return hex.EncodeToString(b)
}

// TestEtcdUpdateValue tests to update value of a key.
// The test coverage includes setting, getting, updating, deleting.
func TestEtcdUpdateValue(t *testing.T) {
	// Use a random key name so members of a cluster don't step on each other.
	target := targetAddress + generateKey() + "?consistent=true"

	stdout, stderr, err := Run("curl", append(retry, "-L", target, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", helloStr))...)
	if err != nil {
		t.Fatalf("curl set failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, helloStr) {
		t.Fatalf("Failed getting value %v\nstdout: %v", helloStr, stdout)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", newHelloStr))...)
	if err != nil {
		t.Fatalf("curl update failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target)...)
	if err != nil {
		t.Fatalf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, newHelloStr) {
		t.Fatalf("Failed getting value %v\nstdout: %v", newHelloStr, stdout)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target, "-XDELETE")...)
	if err != nil {
		t.Fatalf("curl delete failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target)...)
	if err != nil {
		t.Fatalf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, keyNotFound) {
		t.Fatalf("Failed getting value %v\nstdout: %v", keyNotFound, stdout)
	}
}
