package coretest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
func TestEtcdUpdateValue() error {
	// Do not start until etcd is up, 3 is defined in kola/coretest.go
	if err := getClusterHealth(3); err != nil {
		return err
	}

	// Use a random key name so members of a cluster don't step on each other.
	target := targetAddress + generateKey() + "?consistent=true"

	stdout, stderr, err := Run("curl", append(retry, "-L", target, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", helloStr))...)
	if err != nil {
		return fmt.Errorf("curl set failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, helloStr) {
		return fmt.Errorf("Failed getting value %v\nstdout: %v", helloStr, stdout)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target, "-XPUT", "-d", fmt.Sprintf("value=\"%s\"", newHelloStr))...)
	if err != nil {
		return fmt.Errorf("curl update failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target)...)
	if err != nil {
		return fmt.Errorf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, newHelloStr) {
		return fmt.Errorf("Failed getting value %v\nstdout: %v", newHelloStr, stdout)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target, "-XDELETE")...)
	if err != nil {
		return fmt.Errorf("curl delete failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("curl", append(retry, "-L", target)...)
	if err != nil {
		return fmt.Errorf("curl get failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, keyNotFound) {
		return fmt.Errorf("Failed getting value %v\nstdout: %v", keyNotFound, stdout)
	}
	return nil
}

// poll cluster-health until result
func getClusterHealth(csize int) error {
	const (
		retries   = 5
		retryWait = 3 * time.Second
	)
	var err error
	var stdout, stderr string

	for i := 0; i < retries; i++ {
		plog.Info("polling cluster health...")
		stdout, stderr, err = Run("etcdctl", "cluster-health")
		if err == nil {
			break
		}
		time.Sleep(retryWait)
	}
	if err != nil {
		return fmt.Errorf("health polling failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// repsonse should include "healthy" for each machine and for cluster
	if strings.Count(stdout, "healthy") == csize+1 {
		return nil
	} else {
		return fmt.Errorf("status unhealthy or incomplete: stdout: %s\nstderr: %s", err, stdout, stderr)
	}
}
