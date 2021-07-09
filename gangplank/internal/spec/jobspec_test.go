package spec

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// setMockHttpGet sets the httpGet func to a single-use mocking func for returing
// an HTTP tests.
func setMockHttpGet(data []byte, status int, err error) {
	httpGet = func(string) (*http.Response, error) {
		defer func() {
			httpGet = http.Get
		}()
		return &http.Response{
			Body:       ioutil.NopCloser(bytes.NewReader(data)),
			StatusCode: status,
		}, err
	}
}

func TestURL(t *testing.T) {
	tmpd, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create tmpdir")
	}
	defer os.RemoveAll(tmpd) //nolint

	cases := []struct {
		repo       Repo
		desc       string
		data       []byte
		statusCode int
		wantErr    bool
	}{
		{
			desc:       "good repo",
			data:       []byte("good repo"),
			repo:       Repo{URL: strPtr("http://mirrors.kernel.org/fedora-buffet/archive/fedora/linux/releases/30/Everything/source/tree/media.repo")},
			statusCode: 200,
			wantErr:    false,
		},
		{
			desc:       "named repo",
			data:       []byte("named repo"),
			statusCode: 200,
			repo: Repo{
				Name: "test",
				URL:  strPtr("http://mirrors.kernel.org/fedora-buffet/archive/fedora/linux/releases/30/Everything/source/tree/media.repo")},
			wantErr: false,
		},
		{
			desc:       "bad repo",
			data:       nil,
			statusCode: 404,
			repo:       Repo{URL: strPtr("http://mirrors.kernel.org/this/will/not/exist/no/really/it/shouldnt")},
			wantErr:    true,
		},
		{
			desc:    "inline repo",
			repo:    Repo{Inline: strPtr("meh this is a repo")},
			wantErr: false,
		},
		{
			desc: "named inline repo",
			repo: Repo{
				Name:   "named inline repo",
				Inline: strPtr("meh this is a repo"),
			},
			wantErr: false,
		},
	}

	for idx, c := range cases {
		t.Run(fmt.Sprintf("%s case %d", t.Name(), idx), func(t *testing.T) {

			setMockHttpGet(c.data, c.statusCode, nil)

			path, err := c.repo.Writer(tmpd)
			if c.wantErr && err == nil {
				t.Fatalf("%s: wanted error, got none", c.desc)
			}

			wantPath := filepath.Join(tmpd, fmt.Sprintf("%s.repo", c.repo.Name))
			if c.repo.Name == "" {
				h := sha256.New()
				var data []byte
				if c.repo.URL != nil {
					data = []byte(*c.repo.URL)
				} else {
					data = []byte(*c.repo.Inline)
				}
				_, _ = h.Write(data)
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
