package coretest

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func CheckPort(network, address string, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		_, err := net.Dial(network, address)
		errc <- err
	}()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s:%s timed out after %s seconds.",
			network, address, timeout)
	case err := <-errc:
		return err
	}
}

func IsLink(f os.FileInfo) bool {
	return f.Mode()&os.ModeSymlink != 0
}

func CheckHttpStatus(url string, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		tr := &http.Transport{}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			errc <- err
			return
		}

		resp, err := tr.RoundTrip(req)
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			errc <- fmt.Errorf("%s failed with status %d.", url, resp.StatusCode)
			return
		}
		errc <- nil
	}()

	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s timed out after %s seconds.",
			url, timeout)
	case err := <-errc:
		return err
	}
}
