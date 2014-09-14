package index

import (
	"fmt"
	"net/url"
	"strings"

	storage "code.google.com/p/google-api-go-client/storage/v1"
)

type Directory struct {
	Bucket  string
	Prefix  string
	SubDirs map[string]*Directory
	Objects map[string]*storage.Object
}

func NewDirectory(rawURL string) (*Directory, error) {
	gsURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if gsURL.Scheme != "gs" {
		return nil, fmt.Errorf("URL missing gs:// scheme prefix: %q", rawURL)
	}
	if gsURL.Host == "" {
		return nil, fmt.Errorf("URL missing bucket name: %q", rawURL)
	}

	// Object name prefix must never start with / but always end with /
	gsURL.Path = strings.TrimLeft(gsURL.Path, "/")
	if gsURL.Path != "" && !strings.HasSuffix(gsURL.Path, "/") {
		gsURL.Path += "/"
	}

	return &Directory{
		Bucket:  gsURL.Host,
		Prefix:  gsURL.Path,
		SubDirs: make(map[string]*Directory),
		Objects: make(map[string]*storage.Object),
	}, nil
}

func (d *Directory) AddObject(obj *storage.Object) {
	name := strings.TrimPrefix(obj.Name, d.Prefix)
	split := strings.SplitAfterN(name, "/", 2)

	// Save object locally if it has no slash or only ends in slash
	if len(split) == 1 || len(split[1]) == 0 {
		d.Objects[name] = obj
		return
	}

	sub, ok := d.SubDirs[split[0]]
	if !ok {
		sub = &Directory{
			Bucket:  d.Bucket,
			Prefix:  d.Prefix + split[0],
			SubDirs: make(map[string]*Directory),
			Objects: make(map[string]*storage.Object),
		}
		d.SubDirs[split[0]] = sub
	}

	sub.AddObject(obj)
}
