package spec

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestURL(t *testing.T) {
	tmpd, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create tmpdir")
	}
	defer os.RemoveAll(tmpd) //nolint

	cases := []struct {
		repo    Repo
		desc    string
		wantErr bool
	}{
		{
			desc:    "good repo",
			repo:    Repo{URL: "http://mirrors.kernel.org/fedora-buffet/archive/fedora/linux/releases/30/Everything/source/tree/media.repo"},
			wantErr: false,
		},
		{
			desc: "named repo",
			repo: Repo{
				Name: "test",
				URL:  "http://mirrors.kernel.org/fedora-buffet/archive/fedora/linux/releases/30/Everything/source/tree/media.repo"},
			wantErr: false,
		},
		{
			desc:    "bad repo",
			repo:    Repo{URL: "http://fedora.com/this/will/not/exist/no/really/it/shouldnt"},
			wantErr: true,
		},
	}

	for idx, c := range cases {
		t.Run(fmt.Sprintf("%s case %d", t.Name(), idx), func(t *testing.T) {
			path, err := c.repo.Writer(tmpd)
			if c.wantErr && err == nil {
				t.Fatalf("%s: wanted error, got none", c.desc)
			}

			wantPath := filepath.Join(tmpd, fmt.Sprintf("%s.repo", c.repo.Name))
			if c.repo.Name == "" {
				h := sha256.New()
				_, _ = h.Write([]byte(c.repo.URL))
				wantPath = filepath.Join(tmpd, fmt.Sprintf("%x.repo", h.Sum(nil)))
			}
			if wantPath != path {
				t.Fatalf("%s path test:\n wanted: %s\n    got: %s", c.desc, wantPath, path)
			}

			fi, err := os.Stat(path)
			if err != nil {
				t.Fatalf("%s: expected repo %s to be written", c.desc, path)
			}

			if fi.Size() == 0 && !c.wantErr {
				t.Fatalf("%s is not expected to be zero size", path)
			}
		})
	}
}
