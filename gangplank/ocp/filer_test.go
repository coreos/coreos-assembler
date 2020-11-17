package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	log "github.com/sirupsen/logrus"
)

func TestFiler(t *testing.T) {
	tmpd, err := ioutil.TempDir("", "cosa-test")
	if err != nil {
		t.Fatalf("Failed to create tempdir")
	}
	defer os.RemoveAll(tmpd)

	testBucket := "testbucket"
	testFileContents := "this is a test"

	c, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer c.Done()

	m := newMinioServer()
	m.Host = "localhost"
	m.dir = tmpd
	if err := m.start(c); err != nil {
		t.Fatalf("Failed to start minio: %v", err)
	}

	mc, err := m.client()
	if err != nil {
		t.Errorf("Failed to create test minio client")
	}
	defer m.kill()

	if err := mc.MakeBucket(c, testBucket, minio.MakeBucketOptions{}); err != nil {
		t.Errorf("Failed to create test bucket %s: %v", testBucket, err)
	}

	r := strings.NewReader(testFileContents)
	if _, err := mc.PutObject(c, testBucket, "test", r, -1, minio.PutObjectOptions{}); err != nil {
		t.Errorf("Failed to place test file")
	}

	tfp := filepath.Join(tmpd, testBucket, "test")
	f, err := ioutil.ReadFile(tfp)
	if err != nil {
		t.Errorf("Failed to find file: %v", err)
	}
	if string(f) != testFileContents {
		t.Errorf("Test file should be %q, got %q", testFileContents, f)
	}

	testBucket = "tb1"
	if err = m.putter(c, testBucket, "test", tfp, false); err != nil {
		t.Errorf("error: %v", err)
	}

	log.Info("Done")
}
