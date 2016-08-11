package gcloud

import (
	"golang.org/x/crypto/ssh"
)

type machine struct {
	gc    *cluster
	name  string
	intIP string
	extIP string
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

func (gm *machine) Destroy() error {
	if err := gm.gc.api.TerminateInstance(gm.name); err != nil {
		return err
	}

	gm.gc.DelMach(gm)

	return nil
}
