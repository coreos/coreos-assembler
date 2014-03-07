package coretest

import (
	"fmt"
	"strings"
	"testing"
)

const (
	targetAddress = "http://127.0.0.1:4001/v2/keys/message"
	helloStr = "Hello world"
	newHelloStr = "Hello etcd"
	keyNotFound = "Key not found"
)

// TestEtcdUpdateValue tests to update value of a key.
// The test coverage includes setting, getting, updating, deleting.
func TestEtcdUpdateValue(t *testing.T) {
	stdout, stderr, err := Run("curl", "-L", targetAddress, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", helloStr))
	if err != nil {
		t.Fatalf("curl set failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, helloStr) {
		t.Fatalf("Failed getting value %v\nstdout: %v", helloStr, stdout)
	}

	stdout, stderr, err = Run("curl", "-L", targetAddress, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", newHelloStr))
	if err != nil {
		t.Fatalf("curl update failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", "-L", targetAddress)
	if err != nil {
		t.Fatalf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, newHelloStr) {
		t.Fatalf("Failed getting value %v\nstdout: %v", newHelloStr, stdout)
	}

	stdout, stderr, err = Run("curl", "-L", targetAddress, "-XDELETE")
	if err != nil {
		t.Fatalf("curl delete failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", "-L", targetAddress)
	if err != nil {
		t.Fatalf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, keyNotFound) {
		t.Fatalf("Failed getting value %v\nstdout: %v", keyNotFound, stdout)
	}
}
