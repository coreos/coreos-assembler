package ocp

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/containers/podman/v3/pkg/terminal"
	"github.com/coreos/gangplank/internal/spec"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type SSHForwardPort struct {
	Host    string
	User    string
	Key     string
	SSHPort int
}

// getSshMinioForwarder returns an SSHForwardPort from the jobspec
// definition for forwarding a minio server, or nil if forwarding is
// not enabled.
func getSshMinioForwarder(j *spec.JobSpec) *SSHForwardPort {
	if j.Minio.SSHForward == "" {
		return nil
	}
	return &SSHForwardPort{
		Host: j.Minio.SSHForward,
		User: j.Minio.SSHUser,
		Key:  j.Minio.SSHKey,
	}
}

// sshClient is borrowed from libpod, and has been modified to return an sshClient.
func sshClient(user, host, port string, secure bool, identity string) (*ssh.Client, error) {
	var signers []ssh.Signer // order Signers are appended to this list determines which key is presented to server
	if len(identity) > 0 {
		s, err := terminal.PublicKey(identity, []byte(""))
		if err != nil {
			return nil, fmt.Errorf("%w: failed to parse identity %q", err, identity)
		}
		signers = append(signers, s)
	}

	if sock, found := os.LookupEnv("SSH_AUTH_SOCK"); found {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return nil, err
		}

		agentSigners, err := agent.NewClient(c).Signers()
		if err != nil {
			return nil, err
		}
		signers = append(signers, agentSigners...)
	}

	var authMethods []ssh.AuthMethod
	if len(signers) > 0 {
		var dedup = make(map[string]ssh.Signer)
		// Dedup signers based on fingerprint, ssh-agent keys override CONTAINER_SSHKEY
		for _, s := range signers {
			fp := ssh.FingerprintSHA256(s.PublicKey())
			_ = dedup[fp]
			dedup[fp] = s
		}

		var uniq []ssh.Signer
		for _, s := range dedup {
			uniq = append(uniq, s)
		}
		authMethods = append(authMethods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return uniq, nil
		}))
	}

	if len(authMethods) == 0 {
		callback := func() (string, error) {
			pass, err := terminal.ReadPassword("Login password:")
			return string(pass), err
		}
		authMethods = append(authMethods, ssh.PasswordCallback(callback))
	}

	callback := ssh.InsecureIgnoreHostKey()
	if secure {
		if port != "22" {
			host = fmt.Sprintf("[%s]:%s", host, port)
		}
		key := terminal.HostKey(host)
		if key != nil {
			callback = ssh.FixedHostKey(key)
		}
	}

	return ssh.Dial("tcp",
		net.JoinHostPort(host, port),
		&ssh.ClientConfig{
			User:            user,
			Auth:            authMethods,
			HostKeyCallback: callback,
			HostKeyAlgorithms: []string{
				ssh.KeyAlgoRSA,
				ssh.KeyAlgoDSA,
				ssh.KeyAlgoECDSA256,
				ssh.KeyAlgoECDSA384,
				ssh.KeyAlgoECDSA521,
				ssh.KeyAlgoED25519,
			},
			Timeout: 5 * time.Second,
		},
	)
}

// fowardOverSSH forwards the minio connection over SSH.
func (m *minioServer) fowardOverSSH(termCh termChan, errCh chan<- error) error {
	sshPort := 22
	if m.overSSH.SSHPort != 0 {
		sshPort = m.overSSH.SSHPort
	}
	sshport := strconv.Itoa(sshPort)

	l := log.WithFields(log.Fields{
		"remote host": m.overSSH.Host,
		"remote user": m.overSSH.User,
		"port":        m.Port,
	})

	l.Info("Forwarding local port over SSH to remote host")

	client, err := sshClient(m.overSSH.User, m.overSSH.Host, sshport, false, m.overSSH.Key)
	if err != nil {
		return err
	}

	// Open the remote port over SSH
	remotePort, err := client.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", m.Port))
	if err != nil {
		err = fmt.Errorf("%w: failed to open remote port over sshfor proxy", err)
		return err
	}

	// copyIO is a blind copier that copies between source and destination
	copyIO := func(src, dest net.Conn) {
		defer src.Close()  //nolint
		defer dest.Close() //nolint
		_, _ = io.Copy(src, dest)
	}

	// proxy is a helper function that connects the local port to the remoteClient
	proxy := func(conn net.Conn) {
		proxy, err := net.Dial("tcp4", fmt.Sprintf("127.0.0.1:%d", m.Port))
		if err != nil {
			err = fmt.Errorf("%w: failed to open local port for proxy", err)
			errCh <- err
			return
		}
		go copyIO(conn, proxy)
		go copyIO(proxy, conn)
	}

	go func() {
		// When the termination signal is recieved, the defers in copyio will be triggered,
		// resulting in the go-routines exiting.
		<-termCh
		l.Info("Shutting down ssh forwarding")
		errCh <- remotePort.Close()
		errCh <- client.Close()
	}()

	go func() {
		// Loop checking for connections or termination.
		for {
			// For each connection, create a proxy from the remote port to the local port
			remoteClient, err := remotePort.Accept()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.WithError(err).Warn("SSH Client error")
			}
			proxy(remoteClient)
		}
	}()

	return nil
}
