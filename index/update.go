package index

import (
	"net/http"
)

// Arbitrary limit on the number of concurrent calls to WriteIndex
const MAX_WRITERS = 12

func Update(client *http.Client, url string) error {
	root, err := NewDirectory(url)
	if err != nil {
		return err
	}

	if err = root.Fetch(client); err != nil {
		return err
	}

	dirs := make(chan *Directory)
	done := make(chan struct{})
	errc := make(chan error)

	// Feed the directory tree into the writers.
	go func() {
		root.Walk(dirs)
		close(dirs)
	}()

	writer := func() {
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

	for i := 0; i < MAX_WRITERS; i++ {
		go writer()
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
