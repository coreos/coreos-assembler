// +minio
package ocp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestTarballRemote(t *testing.T) {
	tmpd, _ := ioutil.TempDir("", "remotes")
	srvd := filepath.Join(tmpd, "serve")
	srcd := filepath.Join(tmpd, "src")
	destd := filepath.Join(tmpd, "dest")
	defer os.RemoveAll(tmpd) //nolint

	for _, d := range []string{srvd, srcd, destd} {
		if err := os.MkdirAll(d, 0777); err != nil {
			t.Fatalf("failed to create tmpdir")
		}
	}

	// create the fake directory to tar up
	for i := 0; i < 10; i++ {
		tld := filepath.Join(srcd, fmt.Sprintf("%d", i))
		if err := os.MkdirAll(tld, 0777); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		for x := 0; x < 10; x++ {
			rstring, _ := randomString(10 * i * x * i)
			if err := ioutil.WriteFile(filepath.Join(tld, fmt.Sprintf("%d", x)), []byte(rstring), 0644); err != nil {
				t.Fatalf("failed to create entry: %v", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := newMinioServer("")
	m.dir = srvd
	log.Infof("Testing with key %s:%s", m.AccessKey, m.SecretKey)
	_ = m.start(ctx)
	defer m.Kill()

	returner := &Return{
		Minio: m,
	}

	if err := uploadPathAsTarBall(ctx, cacheBucket, "test.tar.gz", srcd, "", false, returner); err != nil {
		t.Fatalf("Failed create tarball: %v", err)
	}

	remoteFile := &RemoteFile{
		Minio:            m,
		Bucket:           cacheBucket,
		Object:           "test.tar.gz",
		Compressed:       true,
		ForceExtractPath: destd,
	}

	if err := remoteFile.Extract(ctx, destd); err != nil {
		t.Fatalf("failed to extract tarball: %v", err)
	}

	walkSrcFn := func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() || path == destd {
			return nil
		}
		srcPath := strings.TrimPrefix(path, destd)
		dest, err := os.Stat(srcPath)
		if err != nil {
			return fmt.Errorf("failed to query %s for %s: %v", srcPath, path, err)
		}
		if dest.Size() != info.Size() {
			return fmt.Errorf("%s size difference:\n want: %d\n  got: %d\n", path, info.Size(), dest.Size())
		}
		return nil
	}

	if err := filepath.Walk(destd, walkSrcFn); err != nil {
		t.Fatalf("failed to validate tarball: %v", err)
	}
}
