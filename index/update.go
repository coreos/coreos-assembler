package index

import (
	"fmt"
	"net/http"

	storage "code.google.com/p/google-api-go-client/storage/v1"
)

func fetch(client *http.Client, url string) (<-chan *Directory, error) {
	service, err := storage.New(client)
	if err != nil {
		return nil, err
	}

	root, err := NewDirectory(url)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Fetching gs://%s/%s\n", root.Bucket, root.Prefix)
	objCount := 0
	listReq := service.Objects.List(root.Bucket)
	if root.Prefix != "" {
		listReq.Prefix(root.Prefix)
	}

	for {
		objs, err := listReq.Do()
		if err != nil {
			return nil, err
		}

		objCount += len(objs.Items)
		fmt.Printf("Found %d objects under gs://%s/%s\n",
			objCount, root.Bucket, root.Prefix)

		for _, obj := range objs.Items {
			root.AddObject(obj)
		}

		if objs.NextPageToken != "" {
			listReq.PageToken(objs.NextPageToken)
		} else {
			break
		}
	}

	dirs := make(chan *Directory)
	go func() {
		streamer(root, dirs)
		close(dirs)
	}()

	return dirs, nil
}

func streamer(dir *Directory, dirs chan<- *Directory) {
	dirs <- dir
	for _, subdir := range dir.SubDirs {
		streamer(subdir, dirs)
	}
}

func Update(client *http.Client, url string) error {
	dirs, err := fetch(client, url)
	if err != nil {
		return err
	}

	errc := make(chan error)

	go func() {
		for d := range dirs {
			if err := d.WriteIndex(client); err != nil {
				errc <- err
				return
			}
		}
		fmt.Printf("Update successful!\n")
		errc <- nil
	}()

	return <-errc
}
