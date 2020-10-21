package ocp

import (
	// minio is needed for moving files around in OpenShift.
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

/*
	Minio (https://github.com/minio/minio) is an S3-API Compatible
	Object Store. When running in multi-pod mode, we start Minio
	for pulling and pushing artifacts. Object Storage is a little better
	than using PVC's.
*/

var minioAccessKey, minioSecretKey string

func startMinio(ctx context.Context, dir string) error {
	mA, err := randomString(12)
	if err != nil {
		return err
	}
	mS, err := randomString(12)
	if err != nil {
		return err
	}
	minioAccessKey = mA
	minioSecretKey = mS
	log.Infof("Starting Minio with %s:%s serving %s",
		minioAccessKey, minioSecretKey, dir)

	args := []string{"minio", "server", dir}
	log.Infof("Minio args %v", args)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(
		os.Environ(),
		fmt.Sprintf("MINIO_ACCESS_KEY=%s", minioAccessKey),
		fmt.Sprintf("MINIO_SECRET_KEY=%s", minioSecretKey),
	)
	if err := cmd.Start(); err != nil {
		stdoutStderr, _ := cmd.CombinedOutput()
		log.Errorf("Failed to create minio:\n%s", stdoutStderr)
		return err
	}
	return nil
}

func randomString(n int) (string, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	bits := make([]byte, n)
	_, err := rand.Read(bits)
	if err != nil {
		return "", err
	}
	for i, b := range bits {
		bits[i] = letters[b%byte(len(letters))]
	}
	return string(bits), nil
}
