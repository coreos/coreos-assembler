package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestRemote(t *testing.T) {
	tmpd, _ := ioutil.TempDir("", "remotes")
	srvd := filepath.Join(tmpd, "serve")
	destd := filepath.Join(tmpd, "in")
	//defer os.RemoveAll(tmpd)

	testBucket := "source"
	if err := os.MkdirAll(filepath.Join(srvd, testBucket), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", srvd)
	}
	if err := os.MkdirAll(destd, 0777); err != nil {
		t.Fatalf("failed to create dir: %v", srvd)
	}
	testContent := "THIS IS A TEST"
	testFilePath := filepath.Join(srvd, testBucket, "test")
	if err := ioutil.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := newMinioServer("")
	m.dir = srvd
	log.Infof("Testing with key %s:%s", m.AccessKey, m.SecretKey)

	if err := m.start(ctx); err != nil {
		t.Fatalf("failed to start test minio server: %v", err)
	}
	defer m.Kill()

	r := RemoteFile{
		Bucket: testBucket,
		Object: "test",
		Minio:  m,
	}

	destF := filepath.Join(destd, "test")
	if err := r.WriteToPath(ctx, destF); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}

	d, err := ioutil.ReadFile(destF)
	if err != nil {
		t.Fatalf("failed to read writen content: %v", err)
	}

	if testContent != string(d) {
		t.Fatalf("test data mismatches")
	}

	r.ForcePath = destF
	if err := r.WriteToPath(ctx, destF); err != nil {
		t.Fatalf("Failed to force write file to path")
	}

	// create an old file to test the stamping
	oldDestF := filepath.Join(destd, "old")
	if err := ioutil.WriteFile(oldDestF, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// stamp and update the file to test if newer
	if err := m.stampFile(testBucket, "test"); err != nil {
		t.Fatalf("Failed to stamp file")
	}
	stamp, err := m.getStamp(testBucket, "test")
	if err != nil {
		t.Fatalf("Failed to get stamp: %v", err)
	}
	if stamp == 0 {
		t.Fatalf("File should have been stamped")
	}
	log.Infof("stamp is %d", stamp)

	time.Sleep(1 * time.Second) // ensure that this is older
	if err := ioutil.WriteFile(destF, []byte("udpated"), 0644); err != nil {
		t.Fatalf("Failed to update the file")
	}
	newer, err := m.isLocalNewer(testBucket, "test", destF)
	if err != nil {
		t.Fatalf("failed to get remote stamp: %v", err)
	}
	cur, err := getLocalFileStamp(destF)
	if err != nil {
		t.Fatalf("failed to get local stamp: %v", err)
	}
	stamp, err = m.getStamp(testBucket, "test")
	if err != nil {
		t.Fatalf("failed to get remote stamp; %v", err)
	}
	if !newer {
		t.Fatalf("local file should be newer: local stamp %d should be larger than remote %d", cur, stamp)
	}
}
