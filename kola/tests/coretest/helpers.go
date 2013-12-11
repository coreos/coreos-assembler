package coretest

import (
	"fmt"
	"net"
	"os"
	"time"
)

func CheckPort(network, address string, timeout time.Duration) error {
	errc := make(chan error)
	go func() {
		_, err := net.Dial(network, address)
		errc <- err
	}()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("%s:%s timed out after %s seconds.",
			network, address, timeout)
	case err := <-errc:
		if err != nil {
			return err
		}
	}
	return nil
}

func IsLink(f os.FileInfo) bool {
	return f.Mode()&os.ModeSymlink != 0
}
