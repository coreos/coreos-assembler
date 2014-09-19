package index

import (
	"fmt"
	"net/http"

	storage "code.google.com/p/google-api-go-client/storage/v1"
)

// Arbitrary limit on the number of concurrent calls to WriteIndex
const MAX_WRITERS = 12

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
			if err := root.AddObject(obj); err != nil {
				return nil, err
			}
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

func writer(client *http.Client, done <-chan struct{}, dirs <-chan *Directory, errc chan<- error) {
	for {
		select {
		case d, ok := <-dirs:
			if !ok {
				errc <- nil
				return
			}
			if !d.NeedsIndex() {
				continue
			}
			if err := d.WriteIndex(client); err != nil {
				errc <- err
				return
			}
		case <-done:
			errc <- nil
			return
		}
	}
}

func Update(client *http.Client, url string) error {
	dirs, err := fetch(client, url)
	if err != nil {
		return err
	}

	done := make(chan struct{})
	errc := make(chan error)

	for i := 0; i < MAX_WRITERS; i++ {
		go writer(client, done, dirs, errc)
	}

	// Wait for writers to finish, aborting and returning the first error.
	var ret error
	for i := 0; i < MAX_WRITERS; i++ {
		err := <-errc
		if err == nil {
			continue
		}
		if done != nil {
			close(done)
			done = nil
		}
		if ret == nil {
			ret = err
		}
	}

	return ret
}
