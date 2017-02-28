package gcloud

import (
	"context"

	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/platform"
)

type machine struct {
	gc      *cluster
	name    string
	intIP   string
	extIP   string
	journal *platform.Journal
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
