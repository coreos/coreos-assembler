package main

import (
	"fmt"
	"net"
	"testing"
	"time"
)

const PortTimeout = time.Second * 3

func checkPort(network, address string, timeout time.Duration) error {
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

func TestPortSsh(t *testing.T) {
	err := checkPort("tcp", "127.0.0.1:22", PortTimeout)
	if err != nil {
		t.Fatal(err)
	}
}
