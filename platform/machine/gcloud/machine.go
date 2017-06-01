package gcloud

import (
	"context"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
)

type machine struct {
	gc      *cluster
	name    string
	intIP   string
	extIP   string
	dir     string
	journal *platform.Journal
	console string
}

func (gm *machine) ID() string {
	return gm.name
}

func (gm *machine) IP() string {
	return gm.extIP
}

func (gm *machine) PrivateIP() string {
	return gm.intIP
}

func (gm *machine) SSHClient() (*ssh.Client, error) {
	return gm.gc.SSHClient(gm.IP())
}

func (gm *machine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return gm.gc.PasswordSSHClient(gm.IP(), user, password)
}

func (gm *machine) SSH(cmd string) ([]byte, error) {
	return gm.gc.SSH(gm, cmd)
}

func (m *machine) Reboot() error {
	if err := platform.StartReboot(m); err != nil {
		return err
	}
	if err := m.journal.Start(context.TODO(), m); err != nil {
		return err
	}
	if err := platform.CheckMachine(m); err != nil {
		return err
	}
	if err := platform.EnableSelinux(m); err != nil {
		return err
	}
	return nil
}

func (gm *machine) Destroy() error {
	if err := gm.saveConsole(); err != nil {
		// log error, but do not fail to terminate instance
		plog.Error(err)
	}

	if err := gm.gc.api.TerminateInstance(gm.name); err != nil {
		return err
	}

	if gm.journal != nil {
		if err := gm.journal.Destroy(); err != nil {
			return err
		}
	}

	gm.gc.DelMach(gm)

	return nil
}

func (gm *machine) ConsoleOutput() string {
	return gm.console
}

func (gm *machine) saveConsole() error {
	var err error
	gm.console, err = gm.gc.api.GetConsoleOutput(gm.name)
	if err != nil {
		return err
	}

	path := filepath.Join(gm.dir, "console.txt")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	f.WriteString(gm.console)

	return nil
}
