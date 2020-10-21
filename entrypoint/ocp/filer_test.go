package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	log "github.com/sirupsen/logrus"
)

func TestFiler(t *testing.T) {
	tmpd, err := ioutil.TempDir("", "cosa-test")
	if err != nil {
		t.Fatalf("Failed to create tempdir")
	}
	defer os.RemoveAll(tmpd)

	testBucket := "test"
	testFileContents := "this is a test"

	ctx, cancel := context.WithCancel(context.Background())
	if err := startMinio(ctx, tmpd); err != nil {
		t.Fatalf("Failed to start minio")
	}
	defer cancel()
	defer ctx.Done()

	objCtx := context.Background()
	mC, err := minio.New("127.0.0.1:9000",
		&minio.Options{
			Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
			Secure: false,
		})
	if err != nil {
		t.Fatalf("Failed to open minio client")
	}

	if err := mC.MakeBucket(objCtx, testBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("Failed to create test bucket")
	}

	r := strings.NewReader(testFileContents)
	if _, err := mC.PutObject(objCtx, testBucket, "test", r, -1, minio.PutObjectOptions{}); err != nil {
		t.Fatalf("Failed to place test file")
	}

	f, err := ioutil.ReadFile(filepath.Join(tmpd, testBucket, "test"))
	if err != nil {
		t.Fatalf("Failed to find file: %v", err)
	}
	if string(f) != testFileContents {
		t.Fatalf("Test file should be %q, got %q", testFileContents, f)
	}
	log.Info("Done")
}
