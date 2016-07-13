package coretest

import (
	"fmt"
	"time"

	"github.com/godbus/dbus"
)

func CheckDbusInterface(iFace string, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		conn, err := dbus.SystemBus()
		if err != nil {
			errc <- err
			return
		}

		msgChan := make(chan *dbus.Message, 10)
		go func(msgChan chan *dbus.Message) {
			<-msgChan
			errc <- nil
			return
		}(msgChan)

		call := conn.BusObject().Call(
			"org.freedesktop.DBus.AddMatch", 0,
			fmt.Sprintf("eavesdrop='true',type=signal,interface=%s", iFace),
		)
		if call.Err != nil {
			errc <- call.Err
		}
		conn.Eavesdrop(msgChan)
	}()

	select {
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s.", timeout)
	case err := <-errc:
		return err
	}
}
