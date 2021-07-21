// +minio
package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/storage/pkg/ioutils"
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

	m := newMinioServer("")
	m.Host = "localhost"
	m.dir = tmpd
	if err := m.start(c); err != nil {
		t.Fatalf("Failed to start minio: %v", err)
	}

	mc, err := m.client()
	if err != nil {
		t.Errorf("Failed to create test minio client: %v", err)
	}
	defer m.Kill()

	if err := mc.MakeBucket(c, testBucket, minio.MakeBucketOptions{}); err != nil {
		t.Errorf("Failed to create test bucket %s: %v", testBucket, err)
	}

	r := strings.NewReader(testFileContents)
	if _, err := mc.PutObject(c, testBucket, "test", r, -1, minio.PutObjectOptions{}); err != nil {
		t.Errorf("Failed to place test file: %v", err)
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
	if err = m.putter(c, testBucket, "test", tfp); err != nil {
		t.Errorf("error: %v", err)
	}

	log.Info("Done")
}

func TestMultipleStandAlones(t *testing.T) {
	tmpd, _ := ioutils.TempDir("", "")
	srvOne := filepath.Join(tmpd, "one")
	srvTwo := filepath.Join(tmpd, "two")
	oneCfg := filepath.Join(srvOne, "test.cfg")
	twoCfg := filepath.Join(srvTwo, "test.cfg")

	_ = os.MkdirAll(srvOne, 0755)
	_ = os.MkdirAll(srvTwo, 0755)

	ctx := context.Background()
	one, err := StartStandaloneMinioServer(ctx, srvOne, oneCfg, nil)
	if err != nil {
		t.Fatalf("%v: failed to start first minio server", err)
	}
	defer one.Kill()

	two, err := StartStandaloneMinioServer(ctx, srvTwo, twoCfg, nil)
	if err != nil {
		t.Fatalf("%v: failed to start first minio server", err)
	}
	defer two.Kill()

	// Fire up the users of the stand alone Minio's
	// This tests using a minio from CFG
	oneUser := newMinioServer(oneCfg)
	if err := oneUser.start(ctx); err != nil {
		t.Errorf("%v: failed to start first minio", err)
	}
	twoUser := newMinioServer(twoCfg)
	if err := twoUser.start(ctx); err != nil {
		t.Errorf("%v: failed to start second minior", err)
	}

	// Connect and make sure that the servers serve different content
	// This tests that:
	//   - host port selection is different
	//   - that a new minio server is not started
	//   - each server is using a different set of keys.
	if err := oneUser.ensureBucketExists(ctx, "test1"); err != nil {
		t.Errorf("%v: failed to create bucket on first minio", err)
	}
	if err := twoUser.ensureBucketExists(ctx, "test2"); err != nil {
		t.Errorf("%v: failed to create bucket on first minio", err)
	}

	oneClient, _ := oneUser.client()
	twoClient, _ := twoUser.client()

	oneBuckets, oneErr := oneClient.ListBuckets(ctx)
	twoBuckets, twoErr := twoClient.ListBuckets(ctx)

	if oneErr != nil || twoErr != nil {
		t.Fatalf("failed to list buckets:\none: %v\ntwo: %v\n", oneErr, twoErr)
	}

	for _, oneV := range oneBuckets {
		for _, twoV := range twoBuckets {
			if oneV.Name == twoV.Name && twoV.Name != "builds" {
				t.Errorf("bucket two %q should not be in minio one", twoV.Name)
			}
		}
	}

	for _, twoV := range twoBuckets {
		for _, oneV := range oneBuckets {
			if oneV.Name == twoV.Name && twoV.Name != "builds" {
				t.Errorf("bucket two %q should not be in minio one", twoV.Name)
			}
		}
	}

	// Try to access oneUser using twoUsers creds
	oneUser.AccessKey = twoUser.AccessKey
	oneUser.SecretKey = twoUser.SecretKey
	if err := oneUser.ensureBucketExists(ctx, "bah"); err == nil {
		t.Fatalf("using wrong credentials should fail")
	}
}
