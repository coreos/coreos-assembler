package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

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

	m := newMinioServer()
	m.dir = srvd
	log.Infof("Testing with key %s:%s", m.AccessKey, m.SecretKey)

	if err := m.start(ctx); err != nil {
		t.Fatalf("failed to start test minio server: %v", err)
	}
	defer m.kill()

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
}
